package engines

import (
	"custom_erp/db"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Stage 95.4/95.5: images for a Design (ProductFamily) and each Combination
// ID (an Item under it), added from the PO they were ordered on or from the
// Design / Item screen. They are ordinary ProductMedia records (pim_media.go),
// so PIM, channel publish, print and the stored-on-the-client's-drive rule
// all apply unchanged. One image per owner is its main image (role "Main
// Image"); the rest are extra images (role "Gallery").

const (
	mainImageRole  = "Main Image"
	extraImageRole = "Gallery"
)

// ImageOwnerDoctype maps what the API sends ("Design" / "ProductFamily" /
// "Item") to the doctype that owns the image.
func ImageOwnerDoctype(d string) (string, error) {
	switch strings.TrimSpace(d) {
	case "ProductFamily", "Design":
		return "ProductFamily", nil
	case "Item", "Combination", "":
		return "Item", nil
	}
	return "", fmt.Errorf("images belong to a Design or an Item, not %q", d)
}

// ListOwnerImages is an owner's Active images, main first.
func ListOwnerImages(tenantID, code string) ([]ProductMediaAsset, error) {
	all, err := ListMediaForItem(tenantID, code)
	if err != nil {
		return nil, err
	}
	out := []ProductMediaAsset{}
	for _, m := range all {
		if strings.HasPrefix(m.FileType, "image/") {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].MediaRole == mainImageRole && out[j].MediaRole != mainImageRole
	})
	return out, nil
}

func ownerRecordCode(tenantID, doctype, code string) (string, error) {
	data, _, err := fetchDocData(tenantID, doctype, code)
	if err != nil {
		what := "item"
		if doctype == "ProductFamily" {
			what = "design"
		}
		return "", &ValidationError{Code: "GLOBAL-0004", SubFor: what, Message: fmt.Sprintf("%s %s was not found", what, code)}
	}
	return firstNonEmpty(strField(data, "code"), strings.TrimSpace(code)), nil
}

// SaveOwnerImage adds one image to a design or a combination. Its first
// image becomes the main one; asMain makes this one the main and keeps the
// old main as an extra. The same file added twice is kept once.
func SaveOwnerImage(tenantID, ownerDoctype, code, userID string, fileBytes []byte, filename string, asMain bool) (*ProductMediaAsset, error) {
	ownerDoctype, err := ImageOwnerDoctype(ownerDoctype)
	if err != nil {
		return nil, err
	}
	code, err = ownerRecordCode(tenantID, ownerDoctype, code)
	if err != nil {
		return nil, err
	}
	if len(fileBytes) == 0 {
		return nil, fmt.Errorf("uploaded file is empty")
	}
	fileType, err := validateMediaFile(filename, fileBytes)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(fileType, "image/") {
		return nil, fmt.Errorf("only JPG, PNG, WebP or GIF images can be added here")
	}
	existing, err := ListOwnerImages(tenantID, code)
	if err != nil {
		return nil, err
	}
	sum := checksumOf(fileBytes)
	hasMain := false
	for _, m := range existing {
		if m.Checksum == sum {
			if asMain && m.MediaRole != mainImageRole {
				if err := SetMainImage(tenantID, m.ID); err != nil {
					return nil, err
				}
				m.MediaRole = mainImageRole
			}
			return &m, nil
		}
		hasMain = hasMain || m.MediaRole == mainImageRole
	}
	role := extraImageRole
	if asMain || !hasMain {
		role = mainImageRole
		if err := demoteMainToExtra(tenantID, code); err != nil {
			return nil, err
		}
	}
	return saveOwnedMediaFile(tenantID, ownerDoctype, fileBytes, filename, code, role, userID, "", "")
}

func setMediaRole(tenantID, mediaID, role string) error {
	data, err := activeMediaRecord(tenantID, mediaID)
	if err != nil {
		return err
	}
	data["media_role"] = role
	marshaled, err := json.Marshal(data)
	if err != nil {
		return err
	}
	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return err
	}
	_, err = db.DB.Exec(fmt.Sprintf(`UPDATE %s.documents SET data = $1, updated_at = CURRENT_TIMESTAMP WHERE doctype = 'ProductMedia' AND id = $2`, schema), marshaled, mediaID)
	return err
}

// demoteMainToExtra keeps an owner's current main image as an extra one -
// unlike a PIM Main Image upload, which retires the old main.
func demoteMainToExtra(tenantID, code string) error {
	list, err := ListMediaForItem(tenantID, code)
	if err != nil {
		return err
	}
	for _, m := range list {
		if m.MediaRole == mainImageRole {
			if err := setMediaRole(tenantID, m.ID, extraImageRole); err != nil {
				return err
			}
		}
	}
	return nil
}

// MediaOwner is the design or item an image belongs to.
func MediaOwner(tenantID, mediaID string) (doctype, code string, err error) {
	data, err := activeMediaRecord(tenantID, mediaID)
	if err != nil {
		return "", "", err
	}
	doctype = strField(data, "owner_doctype")
	if doctype == "" {
		doctype = "Item"
	}
	return doctype, strField(data, "item"), nil
}

// SetMainImage makes an image its owner's main one; the old main stays as an
// extra image.
func SetMainImage(tenantID, mediaID string) error {
	_, code, err := MediaOwner(tenantID, mediaID)
	if err != nil {
		return err
	}
	if err := demoteMainToExtra(tenantID, code); err != nil {
		return err
	}
	return setMediaRole(tenantID, mediaID, mainImageRole)
}

// RemoveOwnerImage takes an image off its design or item (marked Inactive,
// never deleted - DeactivateMedia). Removing the main image makes the next
// one the main.
func RemoveOwnerImage(tenantID, mediaID string) error {
	data, err := activeMediaRecord(tenantID, mediaID)
	if err != nil {
		return err
	}
	if err := DeactivateMedia(tenantID, mediaID); err != nil {
		return err
	}
	if strField(data, "media_role") != mainImageRole {
		return nil
	}
	rest, err := ListOwnerImages(tenantID, strField(data, "item"))
	if err != nil || len(rest) == 0 {
		return err
	}
	return setMediaRole(tenantID, rest[0].ID, mainImageRole)
}

// --- the PO's Images page ---------------------------------------------------

// ImageOwner is a design or a combination on a PO, with its images.
type ImageOwner struct {
	Doctype string              `json:"doctype"`
	Code    string              `json:"code"`
	Name    string              `json:"name,omitempty"`
	Options map[string]string   `json:"options,omitempty"`
	Images  []ProductMediaAsset `json:"images"`
}

// PODesignImages is a design on the PO and the combinations ordered under it.
type PODesignImages struct {
	ImageOwner
	Combinations []ImageOwner `json:"combinations"`
}

// POImages is the PO's Images page: its designs, then any line whose item
// has no design.
type POImages struct {
	PO       string           `json:"po"`
	PONumber string           `json:"po_number"`
	Status   string           `json:"status"`
	Vendor   string           `json:"vendor,omitempty"`
	Storage  string           `json:"storage"` // "drive" or "server"
	Bucket   string           `json:"bucket,omitempty"`
	Designs  []PODesignImages `json:"designs"`
	Items    []ImageOwner     `json:"items"`
}

func (p *POImages) owners() []*ImageOwner {
	out := []*ImageOwner{}
	for i := range p.Designs {
		out = append(out, &p.Designs[i].ImageOwner)
		for j := range p.Designs[i].Combinations {
			out = append(out, &p.Designs[i].Combinations[j])
		}
	}
	for i := range p.Items {
		out = append(out, &p.Items[i])
	}
	return out
}

func poLineSKUs(raw interface{}) []string {
	var lines []map[string]interface{}
	switch v := raw.(type) {
	case string:
		_ = json.Unmarshal([]byte(v), &lines)
	case []interface{}:
		for _, l := range v {
			if m, ok := l.(map[string]interface{}); ok {
				lines = append(lines, m)
			}
		}
	}
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lines {
		sku := strings.TrimSpace(firstNonEmpty(strField(l, "sku"), strField(l, "item_code"), strField(l, "item")))
		if sku != "" && !seen[sku] {
			seen[sku] = true
			out = append(out, sku)
		}
	}
	return out
}

// GetPOImages lists a PO's designs and combinations with their images.
func GetPOImages(tenantID, poID string) (POImages, error) {
	return loadPOImages(tenantID, poID, true)
}

func loadPOImages(tenantID, poID string, withImages bool) (POImages, error) {
	po, status, err := fetchDocData(tenantID, "PurchaseOrder", poID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return POImages{}, &ValidationError{Code: "GLOBAL-0004", SubFor: "Purchase Order", Message: fmt.Sprintf("purchase order %s was not found", poID)}
		}
		return POImages{}, err
	}
	out := POImages{
		PO: poID, PONumber: firstNonEmpty(strField(po, "po_number"), strField(po, "code"), poID),
		Status: firstNonEmpty(strField(po, "status"), status), Vendor: strField(po, "vendor"),
		Storage: "server", Designs: []PODesignImages{}, Items: []ImageOwner{},
	}
	if d, err := loadImageDrive(tenantID); err == nil && d != nil && d.Enabled {
		out.Storage, out.Bucket = "drive", d.Bucket
	}
	// A combination's options are shown by attribute name (Colour), not by
	// the attribute's code.
	labels := map[string]string{}
	if m, err := GetDesignMatrix(tenantID); err == nil {
		for _, a := range m.Attributes {
			labels[strings.ToLower(a.Code)] = a.Label
		}
	}
	optionsOf := func(raw string) map[string]string {
		out := map[string]string{}
		for k, v := range parseOptionPairs(raw) {
			if l := labels[strings.ToLower(k)]; l != "" {
				k = l
			}
			out[k] = v
		}
		return out
	}
	designAt := map[string]int{}
	for _, sku := range poLineSKUs(po["items"]) {
		it, _, err := fetchDocData(tenantID, "Item", sku)
		if err != nil {
			continue // a line whose item is gone has nothing to add an image to
		}
		code := firstNonEmpty(strField(it, "code"), sku)
		combo := ImageOwner{Doctype: "Item", Code: code, Name: strField(it, "name"),
			Options: optionsOf(strField(it, "variant_option_values")), Images: []ProductMediaAsset{}}
		fam := firstNonEmpty(strField(it, "family"), strField(it, "parent_product_code"))
		if fam == "" || fam == code {
			out.Items = append(out.Items, combo)
			continue
		}
		i, ok := designAt[fam]
		if !ok {
			d, _, err := fetchDocData(tenantID, "ProductFamily", fam)
			if err != nil {
				out.Items = append(out.Items, combo)
				continue
			}
			out.Designs = append(out.Designs, PODesignImages{ImageOwner: ImageOwner{
				Doctype: "ProductFamily", Code: firstNonEmpty(strField(d, "code"), fam), Name: strField(d, "name"), Images: []ProductMediaAsset{},
			}, Combinations: []ImageOwner{}})
			i = len(out.Designs) - 1
			designAt[fam] = i
		}
		out.Designs[i].Combinations = append(out.Designs[i].Combinations, combo)
	}
	if withImages {
		for _, o := range out.owners() {
			if o.Images, err = ListOwnerImages(tenantID, o.Code); err != nil {
				return out, err
			}
		}
	}
	return out, nil
}

// normaliseImageName makes a file name and a number comparable: lower case,
// and / \ _ - . space ( ) all one separator - a file cannot be called
// "Design/HO/26-27/000006.jpg", so "Design_HO_26-27_000006.jpg" and
// "design-ho-26-27-000006.jpg" both mean it.
func normaliseImageName(s string) string {
	var b strings.Builder
	sep := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case '/', '\\', '_', '-', '.', ' ', '(', ')':
			if !sep {
				b.WriteByte('-')
				sep = true
			}
			continue
		}
		b.WriteRune(r)
		sep = false
	}
	return strings.Trim(b.String(), "-")
}

// matchImageFile finds the design or combination on the PO a file is named
// after: its full number, or the part after the last "/" (000006,
// 000006-Red-S) when only one owner on the PO has it. The file may add a
// suffix ("000006-Red-S-2", "000006 back"). The longest match wins, so
// 000006-Red-S-2 is the combination and 000006-back the design.
func matchImageFile(owners []*ImageOwner, filename string) (*ImageOwner, string) {
	base := filename
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}
	name := normaliseImageName(base)
	if name == "" {
		return nil, "the file has no name to match"
	}
	shortCount := map[string]int{}
	shortOf := func(code string) string {
		if i := strings.LastIndex(code, "/"); i >= 0 {
			return normaliseImageName(code[i+1:])
		}
		return ""
	}
	for _, o := range owners {
		if s := shortOf(o.Code); s != "" {
			shortCount[s]++
		}
	}
	var best *ImageOwner
	bestLen, tie := 0, false
	for _, o := range owners {
		forms := []string{normaliseImageName(o.Code)}
		if s := shortOf(o.Code); s != "" && shortCount[s] == 1 {
			forms = append(forms, s)
		}
		for _, f := range forms {
			if f == "" || !(name == f || strings.HasPrefix(name, f+"-")) {
				continue
			}
			switch {
			case len(f) > bestLen:
				best, bestLen, tie = o, len(f), false
			case len(f) == bestLen && best != o:
				tie = true
			}
		}
	}
	if best == nil {
		return nil, "no design or combination on this PO is named like this file"
	}
	if tie {
		return nil, "this file name fits more than one design or combination on this PO"
	}
	return best, ""
}

// POImageResult is what happened to one uploaded file.
type POImageResult struct {
	File    string             `json:"file"`
	Matched bool               `json:"matched"`
	Doctype string             `json:"doctype,omitempty"`
	Code    string             `json:"code,omitempty"`
	Reason  string             `json:"reason,omitempty"`
	Image   *ProductMediaAsset `json:"image,omitempty"`
}

func poImagesWritable(p POImages) error {
	if p.Status == "Cancelled" {
		return fmt.Errorf("purchase order %s is cancelled; its images can no longer be changed here", p.PONumber)
	}
	return nil
}

// SavePOImage adds one file to a design or combination on the PO: the one
// named by targetCode, or else the one the file is named after. A file that
// matches nothing comes back Matched=false with the reason - listed, never
// dropped silently.
func SavePOImage(tenantID, poID, userID string, fileBytes []byte, filename, targetDoctype, targetCode string, asMain bool) (POImageResult, error) {
	res := POImageResult{File: filename}
	p, err := loadPOImages(tenantID, poID, false)
	if err != nil {
		return res, err
	}
	if err := poImagesWritable(p); err != nil {
		return res, err
	}
	owners := p.owners()
	var owner *ImageOwner
	if strings.TrimSpace(targetCode) != "" {
		want, err := ImageOwnerDoctype(targetDoctype)
		if err != nil {
			return res, err
		}
		for _, o := range owners {
			if o.Code == strings.TrimSpace(targetCode) && o.Doctype == want {
				owner = o
			}
		}
		if owner == nil {
			return res, fmt.Errorf("%s is not on purchase order %s", targetCode, p.PONumber)
		}
	} else if owner, res.Reason = matchImageFile(owners, filename); owner == nil {
		return res, nil
	}
	img, err := SaveOwnerImage(tenantID, owner.Doctype, owner.Code, userID, fileBytes, filename, asMain)
	if err != nil {
		return res, err
	}
	res.Matched, res.Doctype, res.Code, res.Image = true, owner.Doctype, owner.Code, img
	return res, nil
}

// poImageOnPO checks an image belongs to a design or combination on the PO.
func poImageOnPO(tenantID, poID, mediaID string) error {
	p, err := loadPOImages(tenantID, poID, false)
	if err != nil {
		return err
	}
	if err := poImagesWritable(p); err != nil {
		return err
	}
	_, code, err := MediaOwner(tenantID, mediaID)
	if err != nil {
		return err
	}
	for _, o := range p.owners() {
		if o.Code == code {
			return nil
		}
	}
	return fmt.Errorf("this image is not on purchase order %s", p.PONumber)
}

// SetPOImageMain / RemovePOImage are SetMainImage / RemoveOwnerImage for an
// image on this PO, so the PO's own permission covers them.
func SetPOImageMain(tenantID, poID, mediaID string) error {
	if err := poImageOnPO(tenantID, poID, mediaID); err != nil {
		return err
	}
	return SetMainImage(tenantID, mediaID)
}

func RemovePOImage(tenantID, poID, mediaID string) error {
	if err := poImageOnPO(tenantID, poID, mediaID); err != nil {
		return err
	}
	return RemoveOwnerImage(tenantID, mediaID)
}
