package engines

import (
	"bytes"
	"context"
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Copying server-disk photos to the image drive: copy + verify + switch, a
// read-back that differs keeps the photo on the disk, a re-run skips what is
// done, another client is untouched, and reads work before and after.
func TestImageDriveCopyFromDisk(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const design, itemA, itemB = "S168H-D1", "S168H-I1", "S168H-I2"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ProductMedia' AND data->>'item' LIKE 'S168H%'")
		_, _ = db.DB.Exec("DELETE FROM "+schema+".async_jobs WHERE job_type = $1", imageDriveCopyJobType)
	}
	cleanup()
	defer cleanup()
	clearImageDrive(t, schema)
	defer clearImageDrive(t, schema)
	defer allowPrivateImageDrive()()

	// Photos saved while no drive is set stay on the disk.
	jpgA, jpgB, jpgC := tinyTestJPEG(t, 400, 300), tinyTestJPEG(t, 200, 100), tinyTestJPEG(t, 300, 300)
	save := func(owner, code string, b []byte, name string) string {
		t.Helper()
		a, err := saveOwnedMediaFile("default", owner, b, name, code, "Gallery", "system", "", "")
		if err != nil {
			t.Fatalf("save %s: %v", code, err)
		}
		return a.ID
	}
	idDesign := save("ProductFamily", design, jpgA, "a.jpg")
	idA := save("Item", itemA, jpgB, "b.jpg")
	idB := save("Item", itemB, jpgC, "c.jpg")

	// Another client with a photo on its own disk.
	otherTenant, otherSchema := uniqueLifecycleTenant(t)
	defer dropLifecycleTenant(otherTenant, otherSchema)
	if _, err := ProvisionTenantSchema(otherTenant, otherSchema, "0.1.0-test"); err != nil {
		t.Fatalf("ProvisionTenantSchema: %v", err)
	}
	otherPhoto, err := saveOwnedMediaFile(otherTenant, "Item", jpgA, "o.jpg", "S168H-OTHER", "Gallery", "system", "", "")
	if err != nil {
		t.Skipf("second client cannot store media here: %v", err)
	}

	read := func(id string) []byte {
		t.Helper()
		b, _, _, err := ReadMediaFile("default", id)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		return b
	}
	record := func(sch, id string) map[string]interface{} {
		var raw string
		if err := db.DB.QueryRow("SELECT data FROM "+sch+".documents WHERE id = $1", id).Scan(&raw); err != nil {
			t.Logf("record %s: %v", id, err)
		}
		var m map[string]interface{}
		_ = json.Unmarshal([]byte(raw), &m)
		return m
	}
	if !bytes.Equal(read(idA), jpgB) {
		t.Fatal("before the copy the photo reads from the disk")
	}

	// The drive: corrupts what it hands back for one photo while "bad" is set.
	fake := newFakeS3("erp-images", "AKTEST168", "secret-168")
	var bad atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if bad.Load() && r.Method == http.MethodGet && strings.Contains(r.URL.Path, itemB+"/") && !strings.Contains(r.URL.Path, "_thumb") {
			_, _ = w.Write([]byte("not the photo"))
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer srv.Close()

	// No drive yet: the copy is refused.
	if _, err := StartImageDriveCopy("default", "tester"); err == nil || !strings.Contains(err.Error(), "Store new images") {
		t.Fatalf("no drive: want a refusal, got %v", err)
	}
	if _, err := SaveImageDrive("default", ImageDriveSettings{Enabled: true, Endpoint: srv.URL, Bucket: "erp-images", DesignFolder: "Image/Design Image",
		CombinationFolder: "Image/Combination Image", AccessKeyID: "AKTEST168", SecretAccessKey: "secret-168"}); err != nil {
		t.Fatalf("save drive: %v", err)
	}
	pre, err := PreviewImageDriveCopy("default")
	if err != nil || !pre.DriveOn || pre.OnDisk < 3 || pre.DiskBytes <= 0 {
		t.Fatalf("preview: %+v %v", pre, err)
	}

	bad.Store(true)
	res, err := copyPhotosToImageDrive(context.Background(), "default", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Copied < 2 || res.Failed != 1 || len(res.Failures) != 1 || res.Failures[0].MediaID != idB || !strings.Contains(res.Failures[0].Reason, "differs") {
		t.Fatalf("want the other photos copied and %s kept on the disk with its reason, got %+v", idB, res)
	}
	if m := record(schema, idB); strField(m, "storage") == "drive" || strField(m, "file_path") == "" {
		t.Fatalf("a mismatched photo stays on the disk: %v", m)
	}
	if !bytes.Equal(read(idB), jpgC) {
		t.Fatal("the mismatched photo must still read from the disk")
	}
	mA := record(schema, idA)
	if strField(mA, "storage") != "drive" || strField(mA, "object_key") != "Image/Combination Image/"+itemA+"/"+itemA+".jpg" {
		t.Fatalf("copied photo: want the drive key <folder>/<ID>/<ID>.jpg, got %v", mA)
	}
	if mD := record(schema, idDesign); strField(mD, "object_key") != "Image/Design Image/"+design+"/"+design+".jpg" {
		t.Fatalf("a design photo goes to the design folder, got %v", mD)
	}
	if !bytes.Equal(read(idA), jpgB) || !bytes.Equal(read(idDesign), jpgA) {
		t.Fatal("after the copy the photos read from the drive, unchanged")
	}
	if _, err := ReadMediaThumbnail("default", idA); err != nil {
		t.Errorf("the thumbnail should follow: %v", err)
	}
	// The disk file is kept.
	if key := mediaKeyForStoredPath(strField(mA, "disk_path")); key == "" || !serverMedia.Exists(key) {
		t.Errorf("the disk file must be kept: %v", mA["disk_path"])
	}
	// The other client's photo was not touched.
	if m := record(otherSchema, otherPhoto.ID); strField(m, "storage") == "drive" || strField(m, "file_path") == "" {
		t.Errorf("another client's photo must stay: %v", m)
	}
	before := len(fake.keys())

	// A re-run skips the copied ones, and with the drive honest finishes the failed one.
	bad.Store(false)
	res2, err := copyPhotosToImageDrive(context.Background(), "default", nil)
	if err != nil || res2.Copied != 1 || res2.Failed != 0 {
		t.Fatalf("re-run: want only the failed photo copied, got %+v %v", res2, err)
	}
	if len(fake.keys()) <= before {
		t.Error("the re-run should have added the one photo")
	}
	res3, _ := copyPhotosToImageDrive(context.Background(), "default", nil)
	if res3.Copied != 0 || res3.Failed != 0 {
		t.Fatalf("a third run finds nothing to do: %+v", res3)
	}
	if !bytes.Equal(read(idB), jpgC) {
		t.Fatal("the finished photo reads from the drive")
	}

	// One run at a time.
	if _, err := StartImageDriveCopy("default", "tester"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := StartImageDriveCopy("default", "tester"); err == nil || !strings.Contains(err.Error(), "still going") {
		t.Fatalf("second start while queued: want a refusal, got %v", err)
	}
}
