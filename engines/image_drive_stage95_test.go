package engines

import (
	"bytes"
	"custom_erp/db"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is an in-process S3-compatible bucket. It re-signs every request
// with the expected keys and refuses one whose signature differs, so a test
// that passes really did sign its requests (s3_sigv4_test.go checks the
// signer itself against AWS's published example).
type fakeS3 struct {
	mu      sync.Mutex
	bucket  string
	access  string
	secret  string
	objects map[string][]byte
	types   map[string]string
}

func newFakeS3(bucket, access, secret string) *fakeS3 {
	return &fakeS3{bucket: bucket, access: access, secret: secret, objects: map[string][]byte{}, types: map[string]string{}}
}

func s3XMLError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = w.Write([]byte("<?xml version=\"1.0\"?><Error><Code>" + code + "</Code></Error>"))
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if !strings.Contains(auth, "Credential="+f.access+"/") {
		s3XMLError(w, http.StatusForbidden, "InvalidAccessKeyId")
		return
	}
	at, err := time.Parse("20060102T150405Z", r.Header.Get("X-Amz-Date"))
	if err != nil {
		s3XMLError(w, http.StatusForbidden, "AccessDenied")
		return
	}
	scope := strings.SplitN(strings.SplitN(auth, "Credential=", 2)[1], ",", 2)[0]
	region := strings.Split(scope, "/")[2]
	check, _ := http.NewRequest(r.Method, "http://"+r.Host+r.URL.RequestURI(), nil)
	signed := strings.Split(strings.SplitN(strings.SplitN(auth, "SignedHeaders=", 2)[1], ",", 2)[0], ";")
	for _, h := range signed {
		if h != "host" {
			check.Header.Set(h, r.Header.Get(h))
		}
	}
	s3SignRequest(check, f.access, f.secret, region, r.Header.Get("X-Amz-Content-Sha256"), at)
	if check.Header.Get("Authorization") != auth {
		s3XMLError(w, http.StatusForbidden, "SignatureDoesNotMatch")
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/", 2)
	if parts[0] != f.bucket {
		s3XMLError(w, http.StatusNotFound, "NoSuchBucket")
		return
	}
	key := ""
	if len(parts) == 2 {
		key = parts[1]
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodPut:
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		if sha256Hex(body.Bytes()) != r.Header.Get("X-Amz-Content-Sha256") {
			s3XMLError(w, http.StatusBadRequest, "XAmzContentSHA256Mismatch")
			return
		}
		f.objects[key] = body.Bytes()
		f.types[key] = r.Header.Get("Content-Type")
	case http.MethodGet:
		b, ok := f.objects[key]
		if !ok {
			s3XMLError(w, http.StatusNotFound, "NoSuchKey")
			return
		}
		w.Header().Set("Content-Type", f.types[key])
		_, _ = w.Write(b)
	case http.MethodDelete:
		delete(f.objects, key)
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeS3) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for k := range f.objects {
		out = append(out, k)
	}
	return out
}

// TestStage95SignatureAgreesWithCurl: curl's own SigV4 (--aws-sigv4, the one
// deploy/backup.sh already trusts for off-box backups) signs a PUT, and the
// fake bucket - which checks it with s3SignRequest - must accept it. Skipped
// where curl is missing or too old.
func TestStage95SignatureAgreesWithCurl(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl not on PATH")
	}
	fake := newFakeS3("erp-images", "AKCURL95", "curl-secret/95")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	cmd := exec.Command(curl, "-s", "-o", os.DevNull, "-w", "%{http_code}", "--aws-sigv4", "aws:amz:auto:s3",
		"--user", "AKCURL95:curl-secret/95", "-X", "PUT", "-H", "Content-Type: text/plain", "--data-binary", "hello drive",
		srv.URL+"/erp-images/designs/Design-HO-26-27-000006/a1.txt")
	out, err := cmd.Output()
	if err != nil {
		t.Skipf("curl could not run --aws-sigv4: %v", err)
	}
	if string(out) != "200" {
		t.Fatalf("the fake bucket refused curl's signature: HTTP %s", out)
	}
	if got := string(fake.objects["designs/Design-HO-26-27-000006/a1.txt"]); got != "hello drive" {
		t.Fatalf("object not stored: %q", got)
	}
	// A folder with a space (the client's "Image/Design Image"): curl and
	// s3SignRequest must encode it the same way.
	out, err = exec.Command(curl, "-s", "-o", os.DevNull, "-w", "%{http_code}", "--aws-sigv4", "aws:amz:auto:s3",
		"--user", "AKCURL95:curl-secret/95", "-X", "PUT", "--data-binary", "spaced",
		srv.URL+"/erp-images/Image/Design%20Image/a2.txt").Output()
	if err != nil || string(out) != "200" || string(fake.objects["Image/Design Image/a2.txt"]) != "spaced" {
		t.Fatalf("a folder with a space: HTTP %s, err %v", out, err)
	}
}

// allowPrivateImageDrive turns the on-premise switch on for a test's local
// fake bucket (http on 127.0.0.1); the returned func puts it back.
func allowPrivateImageDrive() func() {
	was := imageDriveAllowPrivate
	imageDriveAllowPrivate = true
	return func() { imageDriveAllowPrivate = was }
}

func clearImageDrive(t *testing.T, schema string) {
	t.Helper()
	_, _ = db.DB.Exec("DELETE FROM "+schema+".channel_credentials WHERE channel_code = $1", imageDriveCredentialCode)
	imageDriveCache.Delete("default")
}

func TestStage95ImageDriveSettings(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	clearImageDrive(t, schema)
	defer clearImageDrive(t, schema)
	defer allowPrivateImageDrive()()
	fake := newFakeS3("erp-images", "AKTEST95", "secret-95")
	srv := httptest.NewServer(fake)
	defer srv.Close()

	good := ImageDriveSettings{Enabled: true, Endpoint: srv.URL + "/", Region: "auto", Bucket: "erp-images",
		DesignFolder: "/Image/ Design Image/", CombinationFolder: "Image/Combination Image",
		AccessKeyID: "AKTEST95", SecretAccessKey: "secret-95"}

	t.Run("a missing bucket is named", func(t *testing.T) {
		in := good
		in.Bucket = ""
		_, err := SaveImageDrive("default", in)
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "PIM-0950" || verr.SubFor != "Bucket" {
			t.Fatalf("want PIM-0950 on Bucket, got %v", err)
		}
	})
	t.Run("both folders are required and must be plain names", func(t *testing.T) {
		for _, bad := range []string{"", " / ", "Image/../secret", "Image/<x>"} {
			in := good
			in.CombinationFolder = bad
			_, err := SaveImageDrive("default", in)
			if verr, ok := err.(*ValidationError); !ok || verr.Code != "PIM-0950" || verr.SubFor != "Combination images folder" {
				t.Fatalf("combination folder %q: want PIM-0950 on it, got %v", bad, err)
			}
		}
		in := good
		in.DesignFolder = ""
		if _, err := SaveImageDrive("default", in); err == nil || !strings.Contains(err.Error(), "design images folder") {
			t.Fatalf("want the design folder named, got %v", err)
		}
	})
	t.Run("without the on-premise switch: https and public addresses only", func(t *testing.T) {
		imageDriveAllowPrivate = false
		defer func() { imageDriveAllowPrivate = true }()
		in := good
		in.Endpoint = "http://storage.example.com"
		if _, err := SaveImageDrive("default", in); err == nil || !strings.Contains(err.Error(), "https://") {
			t.Fatalf("want an https error, got %v", err)
		}
		in.Endpoint = "https://127.0.0.1:9443"
		if _, err := SaveImageDrive("default", in); err == nil || !strings.Contains(err.Error(), "public address") {
			t.Fatalf("want a loopback endpoint refused, got %v", err)
		}
		// The connection itself is checked too, so a name that resolves to a
		// private address after saving is still refused.
		s := good
		s.Endpoint = srv.URL
		if err := newS3Client(&s).checkConnection(); err == nil || !strings.Contains(err.Error(), "non-public address") {
			t.Fatalf("want the dial refused, got %v", err)
		}
	})
	t.Run("switching on with a wrong secret is refused with the reason", func(t *testing.T) {
		in := good
		in.SecretAccessKey = "wrong"
		_, err := SaveImageDrive("default", in)
		if verr, ok := err.(*ValidationError); !ok || verr.Code != "PIM-0951" || !strings.Contains(verr.Message, "secret access key") {
			t.Fatalf("want PIM-0951 naming the secret, got %v", err)
		}
		if v, _ := GetImageDrive("default"); v.Configured {
			t.Fatal("a refused drive must not be saved")
		}
	})
	t.Run("a wrong bucket is named", func(t *testing.T) {
		in := good
		in.Bucket = "other-bucket"
		if err := TestImageDrive("default", in); err == nil || !strings.Contains(err.Error(), `no bucket "other-bucket"`) {
			t.Fatalf("want a no-such-bucket reason, got %v", err)
		}
	})
	t.Run("a working drive saves, tidied, and never gives the secret back", func(t *testing.T) {
		v, err := SaveImageDrive("default", good)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), "secret-95") {
			t.Fatalf("the secret came back: %s", raw)
		}
		if !v.Enabled || !v.SecretSet || v.DesignFolder != "Image/Design Image" || v.CombinationFolder != "Image/Combination Image" || v.Endpoint != srv.URL {
			t.Fatalf("unexpected view %+v", v)
		}
		if len(fake.keys()) != 0 {
			t.Fatalf("Test connection must delete its file, left %v", fake.keys())
		}
	})
	t.Run("a blank secret on a later save keeps the stored one", func(t *testing.T) {
		in := good
		in.SecretAccessKey = ""
		in.CombinationFolder = "Image/Combination Image 2"
		v, err := SaveImageDrive("default", in)
		if err != nil || v.CombinationFolder != "Image/Combination Image 2" || !v.SecretSet {
			t.Fatalf("re-save without the secret: %+v %v", v, err)
		}
	})
}

func TestStage95MatchImageFile(t *testing.T) {
	design := &ImageOwner{Doctype: "ProductFamily", Code: "Design/HO/26-27/000006"}
	red := &ImageOwner{Doctype: "Item", Code: "Design/HO/26-27/000006-Red-S"}
	redM := &ImageOwner{Doctype: "Item", Code: "Design/HO/26-27/000006-Red-M"}
	other := &ImageOwner{Doctype: "ProductFamily", Code: "Design/WH/26-27/000006"}
	owners := []*ImageOwner{design, red, redM}
	cases := []struct {
		file string
		want *ImageOwner
	}{
		{"Design_HO_26-27_000006.jpg", design},
		{"design-ho-26-27-000006-Red-S.png", red},
		{"000006.jpg", design},
		{"000006 back.jpg", design},
		{"000006-Red-S_2.jpg", red},
		{"000006-Red-M (1).jpeg", redM},
		{"000006-Red.jpg", design},
		{"000007.jpg", nil},
		{"photo.jpg", nil},
	}
	for _, c := range cases {
		got, reason := matchImageFile(owners, c.file)
		if got != c.want {
			t.Errorf("%s: got %v (%s), want %v", c.file, got, reason, c.want)
		}
		if got == nil && reason == "" {
			t.Errorf("%s: an unmatched file must say why", c.file)
		}
	}
	// Two designs ending 000006 on one PO: the short form is ambiguous and
	// so not used; the full number still works.
	two := []*ImageOwner{design, other}
	if got, _ := matchImageFile(two, "000006.jpg"); got != nil {
		t.Errorf("a short form two designs share must not match, got %v", got)
	}
	if got, _ := matchImageFile(two, "Design-WH-26-27-000006.jpg"); got != other {
		t.Errorf("the full number must still match, got %v", got)
	}
}

func TestStage95POImagesOnDriveAndServer(t *testing.T) {
	db.InitDB(testConnStr())
	schema, err := db.GetTenantSchema("default")
	if err != nil {
		t.Fatal(err)
	}
	const design, po = "S95TEST/D/000001", "S95TEST-PO-1"
	red, blue := design+"-Red-S", design+"-Blue-S"
	cleanup := func() {
		_, _ = db.DB.Exec("DELETE FROM "+schema+".documents WHERE id IN ($1,$2,$3,$4,$5)", design, red, blue, po, "S95TEST-LOOSE")
		_, _ = db.DB.Exec("DELETE FROM " + schema + ".documents WHERE doctype = 'ProductMedia' AND data->>'item' LIKE 'S95TEST%'")
	}
	cleanup()
	defer cleanup()
	clearImageDrive(t, schema)
	defer clearImageDrive(t, schema)
	ins := func(id, doctype, status string, data map[string]interface{}) {
		raw, _ := json.Marshal(data)
		if _, err := db.DB.Exec("INSERT INTO "+schema+".documents (id, doctype, data, status, created_by) VALUES ($1, $2, $3, $4, 'system')", id, doctype, raw, status); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}
	ins(design, "ProductFamily", "Active", map[string]interface{}{"code": design, "name": "Stage 95 Kurta"})
	ins(red, "Item", "Active", map[string]interface{}{"code": red, "name": "Kurta Red S", "family": design, "variant_option_values": "colour:Red;size:S"})
	ins(blue, "Item", "Active", map[string]interface{}{"code": blue, "name": "Kurta Blue S", "family": design, "variant_option_values": "colour:Blue;size:S"})
	ins("S95TEST-LOOSE", "Item", "Active", map[string]interface{}{"code": "S95TEST-LOOSE", "name": "Loose item"})
	lines, _ := json.Marshal([]map[string]interface{}{{"sku": red, "qty": 2}, {"sku": blue, "qty": 1}, {"sku": red, "qty": 1}, {"sku": "S95TEST-LOOSE", "qty": 1}})
	ins(po, "PurchaseOrder", "Approved", map[string]interface{}{"po_number": po, "vendor": "Test Vendor", "status": "Approved", "items": string(lines)})

	defer allowPrivateImageDrive()()
	fake := newFakeS3("erp-images", "AKTEST95", "secret-95")
	srv := httptest.NewServer(fake)
	defer srv.Close()
	if _, err := SaveImageDrive("default", ImageDriveSettings{Enabled: true, Endpoint: srv.URL, Bucket: "erp-images", DesignFolder: "Image/Design Image", CombinationFolder: "Image/Combination Image",
		AccessKeyID: "AKTEST95", SecretAccessKey: "secret-95"}); err != nil {
		t.Fatalf("save drive: %v", err)
	}

	p, err := GetPOImages("default", po)
	if err != nil {
		t.Fatalf("GetPOImages: %v", err)
	}
	if p.Storage != "drive" || len(p.Designs) != 1 || len(p.Designs[0].Combinations) != 2 || len(p.Items) != 1 {
		t.Fatalf("want 1 design with 2 combinations (a repeated line once) and 1 loose item on the drive, got %+v", p)
	}

	// A file named after the combination goes to it, on the drive, as its main image.
	img := tinyTestJPEG(t, 400, 300)
	res, err := SavePOImage("default", po, "system", img, "000001-Red-S.jpg", "", "", false)
	if err != nil || !res.Matched || res.Code != red || res.Image == nil || res.Image.MediaRole != mainImageRole {
		t.Fatalf("matched upload: %+v %v", res, err)
	}
	var stored map[string]interface{}
	var raw string
	_ = db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", res.Image.ID).Scan(&raw)
	_ = json.Unmarshal([]byte(raw), &stored)
	key := strField(stored, "object_key")
	if strField(stored, "storage") != "drive" || !strings.HasPrefix(key, "Image/Combination Image/S95TEST-D-000001-Red-S/") {
		t.Fatalf("want the image under the combination folder on the drive, got %v", stored)
	}
	if strField(stored, "file_path") != "" {
		t.Fatal("a drive image must not also be written to the ERP server")
	}
	got, fileType, _, err := ReadMediaFile("default", res.Image.ID)
	if err != nil || !bytes.Equal(got, img) || fileType != "image/jpeg" {
		t.Fatalf("read back from the drive: %v", err)
	}
	if th, err := ReadMediaThumbnail("default", res.Image.ID); err != nil || len(th) == 0 {
		t.Fatalf("thumbnail from the drive: %v", err)
	}
	if _, _, err := ReadMediaTransform("default", res.Image.ID, "small"); err != nil {
		t.Fatalf("resized copy: %v", err)
	}
	if _, ok := fake.objects[mediaDerivedKey(key, "small")]; !ok {
		t.Fatal("the resized copy must be kept beside the original on the drive")
	}

	// A second image is an extra one; the same file again is kept once.
	img2 := tinyTestJPEG(t, 200, 200)
	res2, err := SavePOImage("default", po, "system", img2, "000001-Red-S_back.jpg", "", "", false)
	if err != nil || res2.Image.MediaRole != extraImageRole {
		t.Fatalf("second image: %+v %v", res2, err)
	}
	again, err := SavePOImage("default", po, "system", img2, "copy.jpg", "Item", red, false)
	if err != nil || again.Image.ID != res2.Image.ID {
		t.Fatalf("the same file twice must be kept once: %+v %v", again, err)
	}

	// A design image by name; a file matching nothing is listed with why.
	res3, err := SavePOImage("default", po, "system", tinyTestJPEG(t, 120, 90), "000001.jpg", "", "", false)
	if err != nil || res3.Code != design || res3.Doctype != "ProductFamily" {
		t.Fatalf("design image: %+v %v", res3, err)
	}
	var designKey string
	_ = db.DB.QueryRow("SELECT COALESCE(data->>'object_key','') FROM "+schema+".documents WHERE id = $1", res3.Image.ID).Scan(&designKey)
	if !strings.HasPrefix(designKey, "Image/Design Image/S95TEST-D-000001/") {
		t.Fatalf("want the design photo in the design folder, got %q", designKey)
	}
	if b, _, _, err := ReadMediaFile("default", res3.Image.ID); err != nil || len(b) == 0 {
		t.Fatalf("a photo under a folder with spaces must read back: %v", err)
	}
	if miss, err := SavePOImage("default", po, "system", img, "holiday.jpg", "", "", false); err != nil || miss.Matched || miss.Reason == "" {
		t.Fatalf("unmatched file: %+v %v", miss, err)
	}
	if _, err := SavePOImage("default", po, "system", img, "x.jpg", "Item", "NOT-ON-PO", false); err == nil {
		t.Fatal("a target not on the PO must be refused")
	}
	if _, err := SavePOImage("default", po, "system", []byte("%PDF-1.4 not an image"), "000001.pdf", "", "", false); err == nil {
		t.Fatal("a PDF is not an image")
	}

	// Set main keeps the old main as an extra; removing the main promotes the next.
	if err := SetPOImageMain("default", po, res2.Image.ID); err != nil {
		t.Fatalf("set main: %v", err)
	}
	list, _ := ListOwnerImages("default", red)
	if len(list) != 2 || list[0].ID != res2.Image.ID || list[0].MediaRole != mainImageRole || list[1].MediaRole != extraImageRole {
		t.Fatalf("after set main: %+v", list)
	}
	if err := RemovePOImage("default", po, res2.Image.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	list, _ = ListOwnerImages("default", red)
	if len(list) != 1 || list[0].ID != res.Image.ID || list[0].MediaRole != mainImageRole {
		t.Fatalf("after removing the main: %+v", list)
	}

	// Drive switched off: new images go to the ERP server; drive images still read.
	if _, err := SaveImageDrive("default", ImageDriveSettings{Enabled: false, Endpoint: srv.URL, Bucket: "erp-images", DesignFolder: "Image/Design Image", CombinationFolder: "Image/Combination Image",
		AccessKeyID: "AKTEST95"}); err != nil {
		t.Fatalf("switch off: %v", err)
	}
	local, err := SavePOImage("default", po, "system", tinyTestJPEG(t, 90, 90), "000001-Blue-S.jpg", "", "", false)
	if err != nil || local.Code != blue {
		t.Fatalf("server upload: %+v %v", local, err)
	}
	_ = db.DB.QueryRow("SELECT data FROM "+schema+".documents WHERE id = $1", local.Image.ID).Scan(&raw)
	stored = nil
	_ = json.Unmarshal([]byte(raw), &stored)
	if strField(stored, "storage") != "" || strField(stored, "file_path") == "" {
		t.Fatalf("want a server-stored image, got %v", stored)
	}
	if _, err := os.Stat(strField(stored, "file_path")); err != nil {
		t.Fatalf("server file: %v", err)
	}
	if b, _, _, err := ReadMediaFile("default", res.Image.ID); err != nil || !bytes.Equal(b, img) {
		t.Fatalf("a drive image must still read with the drive switched off: %v", err)
	}

	// Settings removed: a drive image says why it cannot be read.
	clearImageDrive(t, schema)
	if _, _, _, err := ReadMediaFile("default", res.Image.ID); err == nil || !strings.Contains(err.Error(), "no image drive") {
		t.Fatalf("want PIM-0952, got %v", err)
	}

	// A cancelled PO's images can no longer be changed.
	_, _ = db.DB.Exec("UPDATE "+schema+".documents SET status = 'Cancelled', data = jsonb_set(data, '{status}', '\"Cancelled\"') WHERE id = $1", po)
	if _, err := SavePOImage("default", po, "system", img, "000001.jpg", "", "", false); err == nil {
		t.Fatal("a cancelled PO must refuse images")
	}
}
