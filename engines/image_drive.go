package engines

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"custom_erp/db"
)

// Stage 95.1: each client's own image drive - an S3-compatible bucket that
// Design and Combination ID images (every ProductMedia upload) are written
// to once the client's admin switches it on in Configuration -> Integrations.
// One connector covers Cloudflare R2, DigitalOcean Spaces, AWS S3, Backblaze
// B2, Wasabi and MinIO: they all take path-style requests
// (<endpoint>/<bucket>/<key>) signed with SigV4 (s3_sigv4.go).
//
// The settings, keys included, are one encrypted entry in channel_credentials
// under "image_drive" - the store courier and channel keys already use - so
// no secret lands in a document, a setting or a migration, and the secret is
// never sent back to the browser. Images uploaded before the drive was on
// stay on the ERP server and keep working: each ProductMedia record says
// where its bytes are (pim_media.go).

const imageDriveCredentialCode = "image_drive"

// ImageDriveSettings is what the admin types. SecretAccessKey left blank on
// a save keeps the one already stored.
type ImageDriveSettings struct {
	Enabled           bool   `json:"enabled"`
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	DesignFolder      string `json:"design_folder"`
	CombinationFolder string `json:"combination_folder"`
	AccessKeyID       string `json:"access_key_id"`
	SecretAccessKey   string `json:"secret_access_key,omitempty"`
}

// ImageDriveView is what the settings screen reads back: never the secret.
type ImageDriveView struct {
	Configured        bool   `json:"configured"`
	Enabled           bool   `json:"enabled"`
	Endpoint          string `json:"endpoint"`
	Region            string `json:"region"`
	Bucket            string `json:"bucket"`
	DesignFolder      string `json:"design_folder"`
	CombinationFolder string `json:"combination_folder"`
	AccessKeyID       string `json:"access_key_id"`
	SecretSet         bool   `json:"secret_set"`
}

func (s *ImageDriveSettings) view() ImageDriveView {
	if s == nil {
		return ImageDriveView{}
	}
	return ImageDriveView{Configured: true, Enabled: s.Enabled, Endpoint: s.Endpoint, Region: s.Region, Bucket: s.Bucket,
		DesignFolder: s.DesignFolder, CombinationFolder: s.CombinationFolder, AccessKeyID: s.AccessKeyID, SecretSet: s.SecretAccessKey != ""}
}

// imageDriveCache keeps each tenant's decrypted settings for a minute, so a
// page of thumbnails is not a credential decrypt per image. A save clears it.
var imageDriveCache sync.Map // tenantID -> imageDriveCacheEntry

type imageDriveCacheEntry struct {
	settings *ImageDriveSettings
	at       time.Time
}

const imageDriveCacheTTL = time.Minute

// loadImageDrive returns the tenant's saved drive, or nil when none is set.
func loadImageDrive(tenantID string) (*ImageDriveSettings, error) {
	if v, ok := imageDriveCache.Load(tenantID); ok {
		if e := v.(imageDriveCacheEntry); time.Since(e.at) < imageDriveCacheTTL {
			return e.settings, nil
		}
	}
	exists, err := HasChannelCredential(tenantID, imageDriveCredentialCode)
	if err != nil {
		return nil, err
	}
	var s *ImageDriveSettings
	if exists {
		fields, err := getChannelCredential(tenantID, imageDriveCredentialCode)
		if err != nil {
			return nil, err
		}
		s = &ImageDriveSettings{
			Enabled: fields["enabled"] == "yes", Endpoint: fields["endpoint"], Region: fields["region"],
			Bucket: fields["bucket"], DesignFolder: fields["design_folder"], CombinationFolder: fields["combination_folder"],
			AccessKeyID: fields["access_key_id"], SecretAccessKey: fields["secret_access_key"],
		}
	}
	imageDriveCache.Store(tenantID, imageDriveCacheEntry{settings: s, at: time.Now()})
	return s, nil
}

// GetImageDrive is the settings screen's read.
func GetImageDrive(tenantID string) (ImageDriveView, error) {
	s, err := loadImageDrive(tenantID)
	if err != nil {
		return ImageDriveView{}, err
	}
	return s.view(), nil
}

var (
	imageDriveBucketRe = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
	// One part of a folder path: the client's own folder names, spaces
	// included ("Image/Design Image").
	imageDriveFolderRe = regexp.MustCompile(`^[A-Za-z0-9 ._()-]+$`)
)

// tidyImageFolder trims each part of a folder path ("Image/ Design Image/"
// -> "Image/Design Image"); ok is false for an empty path or a part that is
// not a plain name.
func tidyImageFolder(raw string) (string, bool) {
	parts := []string{}
	for _, p := range strings.Split(strings.ReplaceAll(raw, `\`, "/"), "/") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if p == "." || p == ".." || !imageDriveFolderRe.MatchString(p) {
			return "", false
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, "/"), len(parts) > 0
}

func imageDriveInvalid(field, msg string) error {
	return &ValidationError{Code: "PIM-0950", SubFor: field, Message: msg}
}

// mergeImageDrive fills a blank secret from the stored settings and checks
// and tidies every field.
func mergeImageDrive(tenantID string, in ImageDriveSettings) (ImageDriveSettings, error) {
	out := ImageDriveSettings{
		Enabled:         in.Enabled,
		Endpoint:        strings.TrimRight(strings.TrimSpace(in.Endpoint), "/"),
		Region:          strings.TrimSpace(in.Region),
		Bucket:          strings.TrimSpace(in.Bucket),
		AccessKeyID:     strings.TrimSpace(in.AccessKeyID),
		SecretAccessKey: strings.TrimSpace(in.SecretAccessKey),
	}
	if out.SecretAccessKey == "" {
		if stored, err := loadImageDrive(tenantID); err == nil && stored != nil {
			out.SecretAccessKey = stored.SecretAccessKey
		}
	}
	if out.Region == "" {
		out.Region = "auto"
	}
	u, err := url.Parse(out.Endpoint)
	if out.Endpoint == "" || err != nil || u.Host == "" || u.RawQuery != "" {
		return out, imageDriveInvalid("Endpoint", "the endpoint must be a full address such as https://<account>.r2.cloudflarestorage.com")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && imageDriveAllowPrivate) {
		return out, imageDriveInvalid("Endpoint", "the endpoint must start with https://")
	}
	if !imageDriveAllowPrivate {
		if err := resolvesToPublicAddressOnly(u.Hostname()); err != nil {
			return out, imageDriveInvalid("Endpoint", "the endpoint must be your storage provider's public address: "+err.Error())
		}
	}
	if !imageDriveBucketRe.MatchString(out.Bucket) {
		return out, imageDriveInvalid("Bucket", "the bucket name is 3-63 lower-case letters, digits, dots or hyphens")
	}
	// Stage 95.9: the client's own two folders - one for design photos, one
	// for combination photos.
	var ok bool
	if out.DesignFolder, ok = tidyImageFolder(in.DesignFolder); !ok {
		return out, imageDriveInvalid("Design images folder", "the design images folder is required - the bucket folder design photos go to, such as Image/Design Image (letters, digits, spaces, . _ - ( ) and /)")
	}
	if out.CombinationFolder, ok = tidyImageFolder(in.CombinationFolder); !ok {
		return out, imageDriveInvalid("Combination images folder", "the combination images folder is required - the bucket folder combination photos go to, such as Image/Combination Image (letters, digits, spaces, . _ - ( ) and /)")
	}
	if out.AccessKeyID == "" {
		return out, imageDriveInvalid("Access key ID", "the access key ID is required")
	}
	if out.SecretAccessKey == "" {
		return out, imageDriveInvalid("Secret access key", "the secret access key is required")
	}
	return out, nil
}

// SaveImageDrive stores the drive. Switching it on runs Test connection
// first, so a drive that cannot take an image is never the one uploads go to.
func SaveImageDrive(tenantID string, in ImageDriveSettings) (ImageDriveView, error) {
	s, err := mergeImageDrive(tenantID, in)
	if err != nil {
		return ImageDriveView{}, err
	}
	if s.Enabled {
		if err := newS3Client(&s).checkConnection(); err != nil {
			return ImageDriveView{}, err
		}
	}
	enabled := "no"
	if s.Enabled {
		enabled = "yes"
	}
	if err := SaveChannelCredential(tenantID, imageDriveCredentialCode, map[string]string{
		"enabled": enabled, "endpoint": s.Endpoint, "region": s.Region, "bucket": s.Bucket,
		"design_folder": s.DesignFolder, "combination_folder": s.CombinationFolder,
		"access_key_id": s.AccessKeyID, "secret_access_key": s.SecretAccessKey,
	}); err != nil {
		return ImageDriveView{}, err
	}
	imageDriveCache.Delete(tenantID)
	return s.view(), nil
}

// TestImageDrive writes a small file to the drive, reads it back and
// deletes it, with what the admin has typed (a blank secret uses the stored
// one). Nothing is saved.
func TestImageDrive(tenantID string, in ImageDriveSettings) error {
	s, err := mergeImageDrive(tenantID, in)
	if err != nil {
		return err
	}
	return newS3Client(&s).checkConnection()
}

// activeImageDrive is where new images go: the tenant's drive when it is
// switched on, else nil (the ERP server's disk).
func activeImageDrive(tenantID string) (*s3Client, error) {
	s, err := loadImageDrive(tenantID)
	if err != nil || s == nil || !s.Enabled {
		return nil, err
	}
	return newS3Client(s), nil
}

// imageDriveForRead reaches the drive an image was stored on - switched off
// for new uploads or not, images already there are still read from it.
func imageDriveForRead(tenantID string) (*s3Client, error) {
	s, err := loadImageDrive(tenantID)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, &ValidationError{Code: "PIM-0952", Message: "this image is on the image drive, and no image drive is set up"}
	}
	return newS3Client(s), nil
}

// --- the S3 client ---------------------------------------------------------

type s3Client struct {
	settings *ImageDriveSettings
	http     *http.Client
}

// imageDriveAllowPrivate lets an on-premise install use storage on its own
// network (a MinIO over http, a private address) - an operator switch, set
// on the server (IMAGE_DRIVE_ALLOW_PRIVATE=1), never by a client. Off (the
// default, and the SaaS setting): https only, and the endpoint must be a
// public address - a client's admin types this URL, so it must not be able
// to point the ERP server at its own network or the cloud metadata service.
// Same rule as webhooks and extension hooks (resolvesToPublicAddressOnly).
var imageDriveAllowPrivate = os.Getenv("IMAGE_DRIVE_ALLOW_PRIVATE") == "1"

// imageDriveDial re-checks the address when each connection is made, so a
// DNS change after the settings were saved cannot get round the rule.
func imageDriveDial(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if imageDriveAllowPrivate {
		return dialer.DialContext(ctx, network, addr)
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, fmt.Errorf("host %q does not resolve", host)
	}
	for _, a := range ips {
		ip := a.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return nil, fmt.Errorf("host %q resolves to a non-public address (%s) - refused", host, ip)
		}
	}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

// imageDriveHTTP talks to image drives only: no proxy (a proxy would dial
// for us and skip the address check), every connection through imageDriveDial.
var imageDriveHTTP = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{
	DialContext: imageDriveDial, TLSHandshakeTimeout: 10 * time.Second, MaxIdleConnsPerHost: 4, IdleConnTimeout: 90 * time.Second,
}}

// imageDriveMaxObject caps a read: an image upload is at most 10 MB.
const imageDriveMaxObject = 32 << 20

var errS3NotFound = errors.New("not found on the image drive")

func newS3Client(s *ImageDriveSettings) *s3Client {
	return &s3Client{settings: s, http: imageDriveHTTP}
}

func (c *s3Client) bucket() string { return c.settings.Bucket }

// folderFor is the client's folder for an owner's photos (Stage 95.9): the
// design folder for a design (ProductFamily), the combination folder for
// every item.
func (c *s3Client) folderFor(ownerDoctype string) string {
	if ownerDoctype == "ProductFamily" {
		return c.settings.DesignFolder
	}
	return c.settings.CombinationFolder
}

// ownerKey is where a photo goes: <folder>/<Design or Combination ID>/<name>,
// "/" in the number written as "-". Records store this full key, so a later
// folder change does not lose photos already written.
func (c *s3Client) ownerKey(ownerDoctype, code, name string) string {
	return c.folderFor(ownerDoctype) + "/" + safeMediaPathPart(code) + "/" + name
}

// ownerPhotoKey names a new photo after its Design or Combination ID (Stage
// 129.9, the user: "rename 1.jpg to D00001.jpg"): <folder>/<ID>/<ID>.jpg, then
// <ID>-2.jpg, <ID>-3.jpg... - one past every name this owner's photos have
// used, so a photo is never written over another and the first one always
// has the same address. Photos stored before keep their own keys.
func (c *s3Client) ownerPhotoKey(tenantID, ownerDoctype, code, ext string) (string, error) {
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return "", err
	}
	base := safeMediaPathPart(code)
	dir := c.folderFor(ownerDoctype) + "/" + base + "/"
	rows, err := db.DB.Query(fmt.Sprintf(`SELECT COALESCE(data->>'object_key', '') FROM %s.documents
		WHERE doctype = 'ProductMedia' AND data->>'item' = $1`, schema), code)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	next := 1
	for rows.Next() {
		var key string
		if rows.Scan(&key) != nil || !strings.HasPrefix(key, dir) {
			continue
		}
		name, n := strings.TrimSuffix(strings.TrimPrefix(key, dir), path.Ext(key)), 0
		if name == base {
			n = 1
		} else if v, err := strconv.Atoi(strings.TrimPrefix(name, base+"-")); err == nil && strings.HasPrefix(name, base+"-") {
			n = v
		}
		if n >= next {
			next = n + 1
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if next > 1 {
		base = fmt.Sprintf("%s-%d", base, next)
	}
	return dir + base + ext, nil
}

// ownerPhotoLocks: two uploads to one Design or Combination ID at the same
// time must not pick the same name.
var ownerPhotoLocks sync.Map

func lockOwnerPhotos(tenantID, code string) func() {
	m, _ := ownerPhotoLocks.LoadOrStore(tenantID+"|"+code, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (c *s3Client) do(method, key string, body []byte, contentType string) (*http.Response, error) {
	target := c.settings.Endpoint + "/" + s3URIEncode(c.settings.Bucket, true) + "/" + s3URIEncode(key, false)
	req, err := http.NewRequest(method, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	payloadHash := s3EmptyPayloadHash
	if len(body) > 0 {
		payloadHash = sha256Hex(body)
	}
	s3SignRequest(req, c.settings.AccessKeyID, c.settings.SecretAccessKey, c.settings.Region, payloadHash, time.Now())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &ValidationError{Code: "PIM-0951", Message: fmt.Sprintf("the image drive at %s could not be reached: %v", c.settings.Endpoint, err)}
	}
	return resp, nil
}

var s3ErrorCodeRe = regexp.MustCompile(`<Code>([^<]+)</Code>`)

// s3Failure turns a refused request into words the admin can act on.
func (c *s3Client) s3Failure(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	code := ""
	if m := s3ErrorCodeRe.FindSubmatch(raw); m != nil {
		code = string(m[1])
	}
	var why string
	switch code {
	case "NoSuchBucket":
		why = fmt.Sprintf("there is no bucket %q at this endpoint", c.settings.Bucket)
	case "InvalidAccessKeyId":
		why = "the drive does not know this access key ID"
	case "SignatureDoesNotMatch":
		why = "the secret access key does not belong to this access key ID"
	case "AccessDenied":
		why = "these keys may not read and write this bucket"
	case "AuthorizationHeaderMalformed", "InvalidRegion", "IllegalLocationConstraintException":
		why = fmt.Sprintf("the region %q is not this bucket's region", c.settings.Region)
	case "RequestTimeTooSkewed":
		why = "the ERP server's clock and the drive's differ too much"
	default:
		why = fmt.Sprintf("the drive answered %s", resp.Status)
		if code != "" {
			why += " (" + code + ")"
		}
	}
	return &ValidationError{Code: "PIM-0951", Message: why}
}

func (c *s3Client) put(key string, body []byte, contentType string) error {
	resp, err := c.do(http.MethodPut, key, body, contentType)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return c.s3Failure(resp)
	}
	return nil
}

func (c *s3Client) get(key string) ([]byte, error) {
	resp, err := c.do(http.MethodGet, key, nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if m := s3ErrorCodeRe.FindSubmatch(raw); m != nil && string(m[1]) == "NoSuchBucket" {
			return nil, &ValidationError{Code: "PIM-0951", Message: fmt.Sprintf("there is no bucket %q at this endpoint", c.settings.Bucket)}
		}
		return nil, errS3NotFound
	}
	if resp.StatusCode/100 != 2 {
		return nil, c.s3Failure(resp)
	}
	return io.ReadAll(io.LimitReader(resp.Body, imageDriveMaxObject))
}

func (c *s3Client) delete(key string) error {
	resp, err := c.do(http.MethodDelete, key, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 && resp.StatusCode != http.StatusNotFound {
		return c.s3Failure(resp)
	}
	return nil
}

// checkConnection is Test connection: in the design folder and in the
// combination folder, write a test file, read it back, delete it.
func (c *s3Client) checkConnection() error {
	for _, folder := range []string{c.settings.DesignFolder, c.settings.CombinationFolder} {
		if err := c.checkFolder(folder); err != nil {
			return err
		}
	}
	return nil
}

func (c *s3Client) checkFolder(folder string) error {
	key := folder + "/" + fmt.Sprintf(".erp-connection-test-%d.txt", time.Now().UnixNano())
	body := []byte("ERP image drive connection test - safe to delete.")
	if err := c.put(key, body, "text/plain"); err != nil {
		return err
	}
	got, err := c.get(key)
	if err != nil {
		if errors.Is(err, errS3NotFound) {
			return &ValidationError{Code: "PIM-0951", Message: "the drive took the test file but could not give it back"}
		}
		return err
	}
	if !bytes.Equal(got, body) {
		return &ValidationError{Code: "PIM-0951", Message: "the drive gave back a different test file than it was sent"}
	}
	return c.delete(key)
}
