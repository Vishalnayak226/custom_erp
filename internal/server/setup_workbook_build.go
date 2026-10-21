package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"custom_erp/db"
	"custom_erp/engines"
)

// Stage 128: the Setup Workbook as a file - the blank template, or this
// organisation's current setup in the same layout, so it can be corrected
// and uploaded again, or copied to start the next client. Each table sheet
// has a header row, a grey hint row starting with "#" (skipped on upload)
// and drop-downs where a column takes a fixed list.

type swCol struct {
	Head    string
	Hint    string
	Width   float64
	Options []string
}

func swT(s string) engines.XLSXCell     { return engines.XLSXCell{Text: s} }
func swNum(f float64) engines.XLSXCell  { return engines.XLSXCell{Num: f, IsNum: true} }
func swBold(s string) engines.XLSXCell  { return engines.XLSXCell{Text: s, Bold: true} }
func swHint(s string) engines.XLSXCell  { return engines.XLSXCell{Text: s, Hint: true} }
func swHeadC(s string) engines.XLSXCell { return engines.XLSXCell{Text: s, Header: true} }

// swRowsBelow is how far below the data a column's drop-down reaches, so
// rows typed in later get it too.
const swRowsBelow = 300

func swTableSheet(cols []swCol, data [][]engines.XLSXCell) engines.XLSXSheet {
	var head, hint []engines.XLSXCell
	var widths []float64
	for i, c := range cols {
		head = append(head, swHeadC(c.Head))
		h := c.Hint
		if i == 0 {
			h = "# " + h
		}
		hint = append(hint, swHint(h))
		w := c.Width
		if w == 0 {
			w = float64(len(c.Head) + 6)
			if w < 14 {
				w = 14
			}
			if w > 40 {
				w = 40
			}
		}
		widths = append(widths, w)
	}
	s := engines.XLSXSheet{ColWidths: widths, FreezeRows: 2, Rows: append([][]engines.XLSXCell{head, hint}, data...)}
	for i, c := range cols {
		if len(c.Options) > 0 {
			s.Validations = append(s.Validations, engines.XLSXListValidation{Col: i, FromRow: 2, ToRow: 2 + len(data) + swRowsBelow, Options: c.Options})
		}
	}
	return s
}

func (run *swRun) buildWorkbook(filled bool) ([]byte, error) {
	sheets := []engines.XLSXSheet{run.instructionsSheet(filled)}
	for _, sh := range setupWorkbookSheets {
		var s engines.XLSXSheet
		var err error
		switch sh.Kind {
		case swDoc:
			s, err = run.docSheet(sh, filled)
		case swSettings:
			s = run.settingsSheet(filled)
		case swSeries:
			s, err = run.seriesSheet(filled)
		case swItemFields:
			s, err = run.itemFieldsSheet(filled)
		case swRoles:
			s = run.rolesSheet(filled)
		case swRoleAccess:
			s = run.roleAccessSheet(filled)
		case swUsers:
			s = run.usersSheet(filled)
		case swApproval:
			s, err = run.approvalSheet(filled)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %v", sh.Name, err)
		}
		s.Name = sh.Name
		sheets = append(sheets, s)
	}
	return engines.WriteXLSXBook(sheets)
}

func (run *swRun) keyText(sh swSheet) string {
	switch sh.Kind {
	case swSettings:
		return "Key (or Setting)"
	case swSeries:
		return "Series"
	case swItemFields:
		return "Key (or Field)"
	case swRoles:
		return "Role"
	case swRoleAccess:
		return "Role + Access to"
	case swUsers:
		return "Username"
	case swApproval:
		return "Record type - its bands are replaced"
	}
	labels := map[string]string{}
	if meta, err := engines.GetDocTypeMeta(run.c.tenantID, sh.Doctype); err == nil {
		for _, f := range meta {
			labels[f.Fieldname] = f.Label
		}
	}
	var alts []string
	for _, alt := range sh.Keys {
		var names []string
		for _, f := range alt {
			if l := labels[f]; l != "" {
				names = append(names, l)
			} else {
				names = append(names, f)
			}
		}
		alts = append(alts, strings.Join(names, " + "))
	}
	return strings.Join(alts, ", else ")
}

func (run *swRun) instructionsSheet(filled bool) engines.XLSXSheet {
	stamp := ""
	if filled {
		stamp = run.c.tenantID
	}
	rows := [][]engines.XLSXCell{
		{swBold("Setup Workbook")},
		{swT("Organisation"), swT(stamp)},
		{swT("Downloaded"), swT(time.Now().Format("2006-01-02 15:04"))},
		{},
		{swBold("How it works")},
		{swT("1"), swT("Fill in the sheets you need. A sheet you leave out, or leave empty, leaves that part of the setup as it is.")},
		{swT("2"), swT("Upload it on Settings → Setup Workbook. Preview lists what every row will do - new, changed (old → new), unchanged, or what is wrong - and changes nothing.")},
		{swT("3"), swT("Apply makes the changes, sheet by sheet in the order of the tabs, through the same checks as the screens. Rows with a problem are listed and the rest go ahead.")},
		{swT("4"), swT("Fix anything and upload the same file again: rows already applied show as unchanged. Download this organisation's setup at any time to start from what is there now.")},
		{},
		{swBold("Rules")},
		{swT("Matching"), swT("Each row is matched to an existing record by its key (table below). A match is updated with the cells you filled; no match creates a new record.")},
		{swT("Blank cells"), swT("A blank cell leaves the value as it is. To empty a value, type (none).")},
		{swT("Never deleted"), swT("Nothing is ever deleted. To stop using a record, set its Status to Inactive.")},
		{swT("Grey row"), swT("The grey row under each header, starting with #, says what the column takes. It is skipped on upload. Columns with a fixed list have a drop-down.")},
		{swT("Links"), swT("Where a column names another record (a store's legal entity, an item name's category), write its name, short code, GSTIN or code. It can be one this workbook creates on an earlier tab.")},
		{swT("Codes"), swT("Codes are given by the system. In a workbook downloaded from this organisation (its id is on row 2), the Code column also finds a record, so you can rename it. In a workbook from another organisation, codes are ignored.")},
		{swT("Users"), swT("A new user gets the First password and must set their own at first sign-in. An existing user's password is never changed. Super Admin cannot be given here.")},
		{swT("Approval rules"), swT("The bands you list for a record type replace its bands. Record types you do not list are left alone.")},
		{swT("Industry"), swT("Set once (Settings sheet, first row). After that it is locked and changed only on Configuration → Industry, with a reason.")},
		{},
		{swHeadC("Sheet"), swHeadC("Matched by"), swHeadC("What it sets up")},
	}
	for _, sh := range setupWorkbookSheets {
		rows = append(rows, []engines.XLSXCell{swT(sh.Name), swT(run.keyText(sh)), swT(sh.About)})
	}
	return engines.XLSXSheet{Name: "Instructions", ColWidths: []float64{22, 60, 80}, Rows: rows}
}

// ---------------------------------------------------------------- records

func (run *swRun) docSheet(sh swSheet, filled bool) (engines.XLSXSheet, error) {
	meta, err := engines.GetDocTypeMeta(run.c.tenantID, sh.Doctype)
	if err != nil {
		return engines.XLSXSheet{}, err
	}
	fields := swDocFields(sh, meta)
	typed := engines.IsTypedCodeMaster(sh.Doctype)
	keys := map[string]bool{}
	for _, alt := range sh.Keys {
		for _, f := range alt {
			keys[f] = true
		}
	}
	var cols []swCol
	for _, f := range fields {
		c := swCol{Head: f.Label}
		var hint []string
		switch {
		case f.Fieldname == "code" && (!typed || sh.Doctype == "HSNCode"):
			hint = append(hint, "Filled in by the system - leave blank for a new row")
		case f.Mandatory && f.Fieldname != "status" && !(sh.Doctype == "ItemName" && f.Fieldname == "name"):
			hint = append(hint, "Required")
		default:
			hint = append(hint, "Optional")
		}
		switch f.Fieldtype {
		case "Select":
			opts := swSelectOptions(f.Options)
			c.Options = opts
			if len(opts) > 0 {
				hint = append(hint, "one of: "+strings.Join(opts, ", "))
			}
		case "Check":
			c.Options = []string{"Yes", "No"}
			hint = append(hint, "Yes or No")
		case "Link":
			if f.Options != "" {
				hint = append(hint, "the name or code of a "+swDoctypeLabel(f.Options))
			}
		case "Number", "Int", "Float", "Currency", "Percent":
			hint = append(hint, "a number")
		case "Date":
			hint = append(hint, "a date, YYYY-MM-DD")
		}
		if sh.Doctype == "ItemName" && f.Fieldname == "name" {
			hint = append(hint, "blank = built from category and type")
		}
		if sh.Doctype == "Location" && f.Fieldname == "cost_center" {
			hint = append(hint, "blank = one is made for the location")
		}
		if f.Fieldname == "attribute_map" {
			hint = append(hint, "attribute codes and values, e.g. COLOR:Red|Gold;POLISH - easier on the screen")
		}
		if sh.Doctype == "Location" && f.Fieldname == "parent" {
			hint = append(hint, "write your head office (the screen fills this in; the workbook does not)")
		}
		if keys[f.Fieldname] {
			hint = append(hint, "finds the record")
		}
		c.Hint = strings.Join(hint, " · ")
		cols = append(cols, c)
	}
	var data [][]engines.XLSXCell
	if filled {
		recs := append([]swRec(nil), run.index(sh.Doctype).recs...)
		sort.SliceStable(recs, func(i, j int) bool {
			return swText(recs[i].Data["code"])+recs[i].ID < swText(recs[j].Data["code"])+recs[j].ID
		})
		for _, rec := range recs {
			var row []engines.XLSXCell
			for _, f := range fields {
				v := rec.Data[f.Fieldname]
				if f.Fieldname == "code" && swText(v) == "" {
					v = rec.ID
				}
				switch f.Fieldtype {
				case "Link":
					if s := swText(v); s != "" && f.Options != "" {
						row = append(row, swT(run.display(f.Options, s)))
					} else {
						row = append(row, swT(s))
					}
				case "Check":
					if s := swText(v); s != "" {
						b, _ := swYesNo(s)
						row = append(row, swT(swYesNoText(b)))
					} else {
						row = append(row, swT(""))
					}
				case "Number", "Int", "Float", "Currency", "Percent":
					if n, err := swNumber(swText(v)); err == nil && swText(v) != "" {
						row = append(row, swNum(n))
					} else {
						row = append(row, swT(swText(v)))
					}
				default:
					row = append(row, swT(swText(v)))
				}
			}
			data = append(data, row)
		}
	}
	return swTableSheet(cols, data), nil
}

// ---------------------------------------------------------------- settings

func (run *swRun) settingsSheet(filled bool) engines.XLSXSheet {
	cols := []swCol{
		{Head: "Module", Hint: "for reading", Width: 16},
		{Head: "Setting", Hint: "for reading", Width: 44},
		{Head: "Value", Hint: "blank = leave as it is", Width: 30},
		{Head: "Key", Hint: "finds the setting - do not change", Width: 34},
		{Head: "What it does", Hint: "for reading", Width: 70},
		{Head: "Default", Hint: "as shipped", Width: 20},
	}
	var data [][]engines.XLSXCell
	var valids []engines.XLSXListValidation
	industry := ""
	var industryNames []string
	for _, ind := range run.industries() {
		industryNames = append(industryNames, strings.TrimSuffix(ind.Name, " Industry"))
	}
	if filled {
		if lock, err := engines.GetIndustryLock(run.c.tenantID); err == nil && lock.Locked {
			industry = lock.IndustryCode
			if ind, ok := swIndustryMatch(run.industries(), lock.IndustryCode); ok {
				industry = strings.TrimSuffix(ind.Name, " Industry")
			}
		}
	}
	data = append(data, []engines.XLSXCell{swT("Industry"), swT("Industry"), swT(industry), swT("industry"),
		swT("Adds the industry's fields, labels and HSN codes. Set once - after that it is locked and changed only on Configuration → Industry, with a reason."), swT("")})
	valids = append(valids, engines.XLSXListValidation{Col: 2, FromRow: 2, ToRow: 2, Options: industryNames})
	all := engines.ListSettingsWithValues(run.c.tenantID)
	sort.SliceStable(all, func(i, j int) bool { return swModuleRank(all[i].Module) < swModuleRank(all[j].Module) })
	for _, s := range all {
		value := ""
		if filled {
			value = swSettingDisplay(s.SettingDefinition, s.Value)
		}
		var opts []string
		switch s.Type {
		case engines.SettingTypeBool:
			opts = []string{"Yes", "No"}
		case engines.SettingTypeSelect:
			for _, o := range s.Options {
				opts = append(opts, swSettingShort(o.Label))
			}
		}
		r := len(data) + 2
		if len(opts) > 0 {
			valids = append(valids, engines.XLSXListValidation{Col: 2, FromRow: r, ToRow: r, Options: opts})
		}
		desc := s.Description
		if s.Unit != "" {
			desc = strings.TrimSpace(desc + " (" + s.Unit + ")")
		}
		data = append(data, []engines.XLSXCell{swT(s.Module), swT(s.Label), swT(value), swT(s.Key), swT(desc), swT(swSettingDisplay(s.SettingDefinition, s.Default))})
	}
	sheet := swTableSheet(cols, data)
	sheet.Validations = valids
	return sheet
}

// swModuleRank lists the settings a purchase order depends on first.
func swModuleRank(module string) int {
	for i, m := range []string{"Procurement", "Inventory", "Security", "Platform", "Localization"} {
		if strings.EqualFold(m, module) {
			return i
		}
	}
	return 10
}

// ---------------------------------------------------------------- number series

func swSeriesRank(key string) int {
	for i, k := range []string{"PO", "GRN", "PRT", "DN", "VCN", "PR", "RFQ", "QTN", "ASN", "LCV", "GRNBatchSeq"} {
		if k == key {
			return i
		}
	}
	return 100
}

func (run *swRun) seriesSheet(filled bool) (engines.XLSXSheet, error) {
	all, err := engines.ListNumberSeries(run.c.tenantID)
	if err != nil {
		return engines.XLSXSheet{}, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		ri, rj := swSeriesRank(all[i].DocType), swSeriesRank(all[j].DocType)
		if ri != rj {
			return ri < rj
		}
		return all[i].DocType < all[j].DocType
	})
	cols := []swCol{
		{Head: "Series", Hint: "finds the series - do not change", Width: 16},
		{Head: "Name", Hint: "for reading", Width: 26},
		{Head: "Prefix", Hint: "capital letters, digits, / and -"},
		{Head: "Separator", Hint: "/, - or None", Options: swPairNames(swSeparatorNames)},
		{Head: "Number length", Hint: "digits, e.g. 5 → 00001"},
		{Head: "Suffix", Hint: "optional"},
		{Head: "Restart", Hint: "how often the count starts again", Width: 22, Options: swPairNames(swRestartNames)},
		{Head: "Year format", Hint: "how the year prints", Options: swPairNames(swYearFormatNames)},
		{Head: "Year position", Hint: "where the year goes", Width: 20, Options: swPairNames(swYearPositionNames)},
		{Head: "Store in number", Hint: "Yes = each store its own count", Width: 18, Options: []string{"Yes", "No"}},
		{Head: "Status", Hint: "Inactive stops the document being saved", Options: []string{"Active", "Inactive"}},
	}
	var data [][]engines.XLSXCell
	for _, s := range all {
		name := swSeriesNames[s.DocType]
		if name == "" {
			name = swDoctypeLabel(s.DocType)
		}
		row := []engines.XLSXCell{swT(s.DocType), swT(name)}
		if filled {
			row = append(row, swT(s.Prefix), swT(swPickName(swSeparatorNames, s.Separator)), swT(fmt.Sprint(s.PaddingWidth)), swT(s.Suffix),
				swT(swPickName(swRestartNames, s.ResetFrequency)), swT(swPickName(swYearFormatNames, s.YearFormat)),
				swT(swPickName(swYearPositionNames, s.YearPosition)), swT(swYesNoText(s.IncludeStore)), swT(swActive(s.ActiveStatus)))
		}
		data = append(data, row)
	}
	return swTableSheet(cols, data), nil
}

// ---------------------------------------------------------------- item fields

func (run *swRun) itemFieldsSheet(filled bool) (engines.XLSXSheet, error) {
	rules, err := engines.ItemFieldRules(run.c.tenantID)
	if err != nil {
		return engines.XLSXSheet{}, err
	}
	cols := []swCol{
		{Head: "Field", Hint: "for reading", Width: 24},
		{Head: "Key", Hint: "finds the field - do not change", Width: 22},
		{Head: "Kind", Hint: "field or attribute", Width: 12},
		{Head: "On the item", Hint: "asked on the Item form", Options: []string{"Yes", "No"}},
		{Head: "In the PO", Hint: "attributes only: a column on the PO line", Options: []string{"Yes", "No"}},
		{Head: "At the GRN", Hint: "asked on every GRN line", Options: swPairNames(swGRNAskNames)},
		{Head: "Values from", Hint: "fields only: an Item Attribute's name, Typed, or Automatic", Width: 24},
		{Head: "Hidden", Hint: "off every form", Options: []string{"Yes", "No"}},
	}
	var data [][]engines.XLSXCell
	for _, r := range rules {
		row := []engines.XLSXCell{swT(r.Label), swT(r.Key), swT(r.Kind)}
		if filled {
			po := ""
			if r.Kind == "attribute" {
				po = swYesNoText(r.PO)
			}
			list := ""
			if r.Kind == "field" {
				list = run.attributeName(r.List)
			}
			row = append(row, swT(swYesNoText(r.Item)), swT(po), swT(swPickName(swGRNAskNames, r.GRN)), swT(list), swT(swYesNoText(r.Hidden)))
		}
		data = append(data, row)
	}
	return swTableSheet(cols, data), nil
}

// ---------------------------------------------------------------- roles and access

func (run *swRun) customRoles() []string {
	var out []string
	if names, err := tenantRoleNames(run.schema); err == nil {
		for _, n := range names {
			if !engines.HasFullAccess(n) {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (run *swRun) rolesSheet(filled bool) engines.XLSXSheet {
	cols := []swCol{
		{Head: "Role", Hint: "Required · finds the role", Width: 24},
		{Head: "Description", Hint: "Optional · for a new role", Width: 40},
		{Head: "Stores", Hint: "which stores its people work at", Width: 20, Options: swPairNames(swRoleStoreNames)},
		{Head: "Selected stores", Hint: "for Selected stores: names or short codes, separated by commas", Width: 40},
	}
	var data [][]engines.XLSXCell
	if filled {
		descs := map[string]string{}
		if rows, err := db.DB.Query(fmt.Sprintf(`SELECT name, COALESCE(description, '') FROM %s.roles`, run.schema)); err == nil {
			for rows.Next() {
				var n, d string
				if rows.Scan(&n, &d) == nil {
					descs[n] = d
				}
			}
			rows.Close()
		}
		for _, role := range run.customRoles() {
			acc, _ := engines.GetRoleStoreAccess(run.c.tenantID, role)
			selected := ""
			if acc.Access == engines.StoreAccessSelected {
				selected = run.storeNames(acc.Stores)
			}
			data = append(data, []engines.XLSXCell{swT(role), swT(descs[role]), swT(swPickName(swRoleStoreNames, acc.Access)), swT(selected)})
		}
	}
	return swTableSheet(cols, data)
}

func (run *swRun) roleAccessSheet(filled bool) engines.XLSXSheet {
	yn := []string{"Yes", "No"}
	cols := []swCol{
		{Head: "Role", Hint: "Required", Width: 24},
		{Head: "Access to", Hint: "a module (Procurement, Inventory...) or one record type (Purchase Order)", Width: 30},
		{Head: "Read", Hint: "Yes or No · blank = as it is", Options: yn},
		{Head: "Create", Hint: "Yes includes Read", Options: yn},
		{Head: "Update", Hint: "Yes includes Read", Options: yn},
		{Head: "Delete", Hint: "Yes includes Read", Options: yn},
	}
	var data [][]engines.XLSXCell
	if filled {
		grants := run.grants()
		modules := run.doctypeModules()
		byModule := map[string][]string{}
		for dt, m := range modules {
			byModule[m] = append(byModule[m], dt)
		}
		var moduleNames []string
		for m := range byModule {
			moduleNames = append(moduleNames, m)
			sort.Strings(byModule[m])
		}
		sort.Strings(moduleNames)
		for _, role := range run.customRoles() {
			g := grants[swNorm(role)]
			for _, m := range moduleNames {
				dts := byModule[m]
				first, uniform := g[dts[0]], true
				for _, dt := range dts[1:] {
					uniform = uniform && g[dt] == first
				}
				if uniform && m != "" {
					if first != (swGrant{}) {
						data = append(data, []engines.XLSXCell{swT(role), swT(m), swT(swYesNoText(first.Read)), swT(swYesNoText(first.Create)), swT(swYesNoText(first.Update)), swT(swYesNoText(first.Delete))})
					}
					continue
				}
				for _, dt := range dts {
					if x := g[dt]; x != (swGrant{}) {
						data = append(data, []engines.XLSXCell{swT(role), swT(swDoctypeLabel(dt)), swT(swYesNoText(x.Read)), swT(swYesNoText(x.Create)), swT(swYesNoText(x.Update)), swT(swYesNoText(x.Delete))})
					}
				}
			}
		}
	}
	return swTableSheet(cols, data)
}

func (run *swRun) usersSheet(filled bool) engines.XLSXSheet {
	cols := []swCol{
		{Head: "Username", Hint: "Required · finds the user", Width: 20},
		{Head: "Email", Hint: "Optional", Width: 28},
		{Head: "Role", Hint: "Required for a new user · not Super Admin", Width: 20},
		{Head: "Home store", Hint: "a store's name or short code", Width: 22},
		{Head: "Department", Hint: "a department's name", Width: 18},
		{Head: "First password", Hint: "new users only, 8+ characters · they set their own at first sign-in · never changes an existing user's", Width: 22},
		{Head: "Store access", Hint: "blank = as it is", Width: 20, Options: swPairNames(swUserStoreNames)},
		{Head: "Stores", Hint: "for Only these stores: names or short codes, separated by commas", Width: 30},
		{Head: "Status", Hint: "Active or Inactive", Options: []string{"Active", "Inactive"}},
	}
	var data [][]engines.XLSXCell
	if filled {
		for _, u := range run.users() {
			stores := ""
			if u.StoreAccess == engines.StoreAccessSelected {
				stores = run.storeNames(u.Stores)
			}
			dept := ""
			if u.Department != "" {
				dept = run.display("Department", u.Department)
			}
			data = append(data, []engines.XLSXCell{swT(u.Username), swT(u.Email), swT(u.Role), swT(run.display("Location", u.LocationCode)), swT(dept), swT(""),
				swT(swPickName(swUserStoreNames, u.StoreAccess)), swT(stores), swT(u.Status)})
		}
	}
	return swTableSheet(cols, data)
}

func (run *swRun) approvalSheet(filled bool) (engines.XLSXSheet, error) {
	cols := []swCol{
		{Head: "Record type", Hint: "Purchase Order, Purchase Requisition, GRN Excess Approval...", Width: 30},
		{Head: "From amount", Hint: "a number"},
		{Head: "To amount", Hint: "blank = no upper limit"},
		{Head: "Approved by", Hint: "a role", Width: 22},
	}
	var data [][]engines.XLSXCell
	if filled {
		rules, err := engines.GetApprovalRules(run.c.tenantID)
		if err != nil {
			return engines.XLSXSheet{}, err
		}
		sort.SliceStable(rules, func(i, j int) bool {
			if rules[i].Doctype != rules[j].Doctype {
				return rules[i].Doctype < rules[j].Doctype
			}
			return rules[i].MinAmount < rules[j].MinAmount
		})
		for _, r := range rules {
			to := swT("")
			if r.MaxAmount != nil {
				to = swNum(*r.MaxAmount)
			}
			data = append(data, []engines.XLSXCell{swT(swDoctypeLabel(r.Doctype)), swNum(r.MinAmount), to, swT(r.RequiredRole)})
		}
	}
	return swTableSheet(cols, data), nil
}
