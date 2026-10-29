package engines

import (
	"regexp"
	"sort"
)

// Stage 49.6.2 - data minimization: the field-purpose register.
//
// data_classification.go (49.6.1) classifies data by CATEGORY; it says nothing
// about the individual fields a doctype collects, and most personal fields
// (a customer's phone, gender or anniversary, a driver's phone at the gate)
// belong to no sensitive-field category at all. This is the field-by-field
// answer to "why do we still collect this": one row per personal or
// personal-looking doctype field, with its purpose, the basis it rests on,
// how long it stays and who reads it.
//
// Scope: every field of the three doctypes that ARE people (Customer,
// Employee, Salesman), and every field elsewhere whose name looks like
// personal data (personalDataFieldPattern). Business data that only looks
// personal - a store's address, the company's bank account - is listed too,
// as BasisNotPersonal, so the decision is written down rather than assumed.
// TestStage4962EveryPersonalFieldHasARegisteredPurpose (DB-backed) fails when
// a doctype gains a matching field with no row here, or a row names a field
// that no longer exists. Employee-linked records whose fields are already
// classified by category (Payslip, SalaryStructure, EmployeeLoan, Grievance)
// are governed by data_classification.go, not repeated here; fields kept only
// inside a document's JSON without a doctype_fields row are not reached by
// this register.
//
// Review is the minimization question a row leaves for the privacy owner -
// a field that has a purpose today but where collecting less looks possible.
// Nothing here deletes or stops collecting anything: that is the owner's call.

// Legal bases. These name the DPDP Act 2023's grounds in plain words; which
// clause of section 7 a "statutory" or "employment" field rests on is for
// counsel to confirm (Stage 47.16 header).
const (
	BasisConsent     = "consent"               // DPDP s.6 - given for a stated purpose, can be withdrawn
	BasisVoluntary   = "voluntary-for-purpose" // given by the person for the transaction itself (s.7 legitimate use)
	BasisEmployment  = "employment"            // held about staff for employment purposes (s.7 legitimate use)
	BasisStatutory   = "statutory"             // a law requires it on the record (GST, income tax, Companies Act)
	BasisNotPersonal = "not-personal"          // a business's own data that only looks personal
)

// PersonalDataField is one row of the register.
type PersonalDataField struct {
	Kind       string // identity, contact, address, demographic, preference, financial, consent, commercial, record, business
	Purpose    string
	LegalBasis string
	Retention  string
	Readers    string
	Review     string // a minimization question for the privacy owner; "" when none
}

// personalDataFieldPattern is what "looks like personal data" means for a
// field name: a contact, address, identity, demographic or financial token as
// a whole word of the snake_case name, or a person-name field.
var personalDataFieldPattern = regexp.MustCompile(`(?i)(^|_)(phone|mobile|whatsapp|sms|email|address|pincode|zip|city|dob|birth|anniversary|gender|pan|aadhaar|aadhar|gstin|bank|ifsc|passport|voter|driving|license|licence|vehicle|uan|esic|salary|father|mother|spouse|religion|caste|blood|medical|health|biometric|photo|signature|ip|latitude|longitude|username|consent)($|_)|(^|_)account_(no|number)$|(^|_)(customer|driver|party|contact|employee|recipient|consignee|nominee|guardian|first|last|full|middle|person)_name$`)

// personDoctypes are the doctypes whose every field is about a person.
var personDoctypes = map[string]bool{"Customer": true, "Employee": true, "Salesman": true}

// Retention answers shared by many rows.
const (
	retainCustomer = "privacy.personal: until consent withdrawal or account closure, then anonymised (privacy_rights.go)"
	retainEmployee = "employment end, then the statutory HR retention [needs decision: 47.16.2]"
	retainPayroll  = "hr.payroll: employment end, statutory payroll retention, then anonymised"
	retainVendor   = "finance.bank / vendor relationship end [needs decision: 47.16.2]"
	retainRecord   = "kept with the document for its statutory period (GST 72 months, Companies Act 8 years) [needs decision: 47.16.4]"
	retainShipment = "kept with the order / shipment record for its statutory period [needs decision: 47.16.4]"
	retainUser     = "while the schedule or request exists; removed with it"
	retainBusiness = "life of the business record"
	readersPOS     = "every operational role (POS and CRM look-up)"
	readersManager = "Store Manager and Super Admin (sensitive_fields.go)"
	readersAdmin   = "Super Admin only (sensitive_fields.go)"
	readersHR      = "HR and Super Admin"
	readersOps     = "warehouse, dispatch and store roles"
	readersFinance = "finance roles and Super Admin"
)

func pdField(kind, purpose, basis, retention, readers string) PersonalDataField {
	return PersonalDataField{Kind: kind, Purpose: purpose, LegalBasis: basis, Retention: retention, Readers: readers}
}

func (f PersonalDataField) review(q string) PersonalDataField { f.Review = q; return f }

func businessField(purpose string) PersonalDataField {
	return pdField("business", purpose, BasisNotPersonal, retainBusiness, "roles that read the master")
}

// personalDataRegister is doctype -> field -> row.
var personalDataRegister = map[string]map[string]PersonalDataField{
	"Customer": {
		"code":                pdField("identity", "Key every sale, invoice and loyalty entry refers to", BasisVoluntary, retainCustomer, readersPOS),
		"name":                pdField("identity", "Name on the bill and the customer look-up", BasisVoluntary, retainCustomer, readersPOS),
		"phone":               pdField("contact", "The till's customer look-up key; e-receipt and order updates", BasisVoluntary, retainCustomer, readersPOS),
		"phone_country":       pdField("contact", "Dialling code that makes phone valid for messaging", BasisVoluntary, retainCustomer, readersPOS),
		"email":               pdField("contact", "E-receipt and order updates", BasisVoluntary, retainCustomer, readersPOS),
		"address":             pdField("address", "Delivery and the address printed on a B2B invoice", BasisVoluntary, retainCustomer, readersPOS),
		"address_line2":       pdField("address", "Delivery and the address printed on a B2B invoice", BasisVoluntary, retainCustomer, readersPOS),
		"city":                pdField("address", "Delivery; CRM segment by city", BasisVoluntary, retainCustomer, readersPOS),
		"state":               pdField("address", "GST place of supply", BasisStatutory, retainCustomer, readersPOS),
		"state_code":          pdField("address", "GST place of supply (state code on the invoice)", BasisStatutory, retainCustomer, readersPOS),
		"pincode":             pdField("address", "Delivery serviceability and courier booking", BasisVoluntary, retainCustomer, readersPOS),
		"pan":                 pdField("identity", "Income-tax PAN quoted on high-value sales (Rule 114B)", BasisStatutory, retainCustomer, readersPOS).review("Needed only above the Rule 114B threshold - consider asking for it only then, not on every customer"),
		"gstin":               pdField("identity", "Buyer GSTIN on a B2B tax invoice", BasisStatutory, retainCustomer, readersPOS),
		"date_of_birth":       pdField("demographic", "Birthday offers and lifecycle campaigns (crm_lifecycle.go)", BasisConsent, retainCustomer, readersManager).review("Used only for birthday campaigns - collect it only from customers who agreed to marketing"),
		"date_of_anniversary": pdField("demographic", "Anniversary offers and lifecycle campaigns (crm_lifecycle.go)", BasisConsent, retainCustomer, readersPOS).review("Used only for campaigns - collect only with marketing consent, and restrict it like date_of_birth"),
		"gender":              pdField("demographic", "CRM segments and the customer dashboard breakdown", BasisConsent, retainCustomer, readersPOS).review("Used only for segmentation and a dashboard split - optional at capture, never required"),
		"sizes":               pdField("preference", "Size preferences for clienteling and recommendations", BasisConsent, retainCustomer, readersPOS),
		"preferred_store":     pdField("preference", "Clienteling and CRM segment", BasisVoluntary, retainCustomer, readersPOS),
		"preferred_salesman":  pdField("preference", "Clienteling - who serves this customer", BasisVoluntary, retainCustomer, readersPOS),
		"tags":                pdField("preference", "CRM labels set by staff", BasisVoluntary, retainCustomer, readersPOS),
		"clevertap_segments":  pdField("preference", "Segments synced from the marketing platform", BasisConsent, retainCustomer, readersPOS),
		"loyalty_points":      pdField("commercial", "Loyalty balance the customer redeems", BasisVoluntary, retainCustomer, readersPOS),
		"loyalty_tier":        pdField("commercial", "Loyalty tier benefits at the till", BasisVoluntary, retainCustomer, readersPOS),
		"price_list_code":     pdField("commercial", "Customer-specific pricing", BasisVoluntary, retainCustomer, readersPOS),
		"credit_limit":        pdField("commercial", "Credit-sale control", BasisVoluntary, retainCustomer, readersPOS),
		"status":              pdField("record", "Active / inactive record state", BasisVoluntary, retainCustomer, readersPOS),
		"merged_into":         pdField("record", "Points a merged duplicate at the surviving customer", BasisVoluntary, retainCustomer, readersPOS),
		"walk_in":             pdField("record", "Marks a store's anonymous walk-in placeholder", BasisVoluntary, retainCustomer, readersPOS),
		"walk_in_store":       pdField("record", "The store a walk-in placeholder belongs to", BasisVoluntary, retainCustomer, readersPOS),
		"consent_sms":         pdField("consent", "Marketing consent for SMS (crm_consent.go send gate)", BasisConsent, retainCustomer, readersPOS),
		"consent_whatsapp":    pdField("consent", "Marketing consent for WhatsApp", BasisConsent, retainCustomer, readersPOS),
		"consent_email":       pdField("consent", "Marketing consent for e-mail", BasisConsent, retainCustomer, readersPOS),
		"consent_updated_at":  pdField("consent", "When consent last changed - proof for the consent record", BasisConsent, retainCustomer, readersPOS),
		"consent_source":      pdField("consent", "Where consent was given - proof for the consent record", BasisConsent, retainCustomer, readersPOS),
	},
	"Employee": {
		"code":              pdField("identity", "Key for attendance, payroll and access", BasisEmployment, retainEmployee, readersHR),
		"name":              pdField("identity", "Name on HR records and payslips", BasisEmployment, retainEmployee, readersHR),
		"department":        pdField("record", "Reporting line and cost allocation", BasisEmployment, retainEmployee, readersHR),
		"designation":       pdField("record", "Job title on the HR record", BasisEmployment, retainEmployee, readersHR).review("No report or workflow reads it - only the record form shows it"),
		"location":          pdField("record", "Work location for attendance and store access", BasisEmployment, retainEmployee, readersHR),
		"reporting_manager": pdField("record", "Approval routing and the HR roster report", BasisEmployment, retainEmployee, readersHR),
		"user_id":           pdField("identity", "Links the employee to their sign-in account", BasisEmployment, retainEmployee, readersHR),
		"status":            pdField("record", "Active / exited record state", BasisEmployment, retainEmployee, readersHR),
		"date_of_joining":   pdField("record", "Service period for statutory benefits", BasisEmployment, retainEmployee, readersHR),
		"date_of_exit":      pdField("record", "Service period; final settlement", BasisEmployment, retainEmployee, readersHR),
		"bank_account_no":   pdField("financial", "Salary payment", BasisEmployment, retainPayroll, readersAdmin),
		"bank_ifsc":         pdField("financial", "Salary payment", BasisEmployment, retainPayroll, readersAdmin),
	},
	"Salesman": {
		"name":       pdField("identity", "Sales attribution and incentive", BasisEmployment, retainEmployee, readersPOS),
		"store":      pdField("record", "The store the salesman sells at", BasisEmployment, retainEmployee, readersPOS),
		"phone":      pdField("contact", "Reach the salesman about a customer or order", BasisEmployment, retainEmployee, readersPOS).review("Shown wherever salesmen are listed - consider restricting it to managers"),
		"is_default": pdField("record", "The store's default salesman at the till", BasisEmployment, retainEmployee, readersPOS),
		"status":     pdField("record", "Active / inactive record state", BasisEmployment, retainEmployee, readersPOS),
	},

	// --- Copies of a person's details on a transaction ---------------------
	"POSInvoice":   {"customer_phone": pdField("contact", "E-receipt and the bill look-up when the sale has no customer record", BasisVoluntary, retainRecord, readersPOS)},
	"SalesInvoice": {"customer_gstin": pdField("identity", "Buyer GSTIN on the tax invoice", BasisStatutory, retainRecord, readersFinance)},
	"SalesOrder": {
		"customer_name":          pdField("identity", "Deliver the order and print the label", BasisVoluntary, retainShipment, readersOps),
		"customer_phone":         pdField("contact", "Courier contact for the delivery", BasisVoluntary, retainShipment, readersOps),
		"shipping_address":       pdField("address", "Delivery address", BasisVoluntary, retainShipment, readersOps),
		"shipping_address_line2": pdField("address", "Delivery address", BasisVoluntary, retainShipment, readersOps),
		"shipping_city":          pdField("address", "Delivery address", BasisVoluntary, retainShipment, readersOps),
		"shipping_pincode":       pdField("address", "Delivery address and courier serviceability", BasisVoluntary, retainShipment, readersOps),
	},
	"LogisticsBooking": {"destination_pincode": pdField("address", "Courier rate and serviceability", BasisVoluntary, retainShipment, readersOps)},
	"DeliveryChallan": {
		"party_name":    pdField("identity", "Consignee on the delivery challan (GST Rule 55)", BasisStatutory, retainRecord, readersOps),
		"party_address": pdField("address", "Consignee address on the delivery challan", BasisStatutory, retainRecord, readersOps),
		"party_gstin":   pdField("identity", "Consignee GSTIN on the delivery challan", BasisStatutory, retainRecord, readersOps),
		"vehicle_no":    pdField("identity", "Vehicle on the challan / e-way bill", BasisStatutory, retainRecord, readersOps),
	},

	// --- Third parties at the dock ----------------------------------------
	"GatePass": {
		"driver_name":    pdField("identity", "Who took the goods out of the gate", BasisVoluntary, retainRecord, readersOps),
		"driver_phone":   pdField("contact", "Reach the driver about the dispatch", BasisVoluntary, retainRecord, readersOps).review("Needed on the day of dispatch only - consider clearing it once the gate pass is closed"),
		"vehicle_number": pdField("identity", "Vehicle that took the goods out", BasisVoluntary, retainRecord, readersOps),
	},
	"GRN":         {"vehicle_no": pdField("identity", "Vehicle that delivered the goods", BasisVoluntary, retainRecord, readersOps)},
	"YardCheckIn": {"driver_name": pdField("identity", "Who is in the yard", BasisVoluntary, retainRecord, readersOps)},
	"Trailer":     {"license_plate": pdField("identity", "Identify the trailer in the yard", BasisVoluntary, retainBusiness, readersOps)},

	// --- Staff addresses for reports and requests -------------------------
	"DashboardDigest":      {"recipient_email": pdField("contact", "Deliver the scheduled digest", BasisEmployment, retainUser, "the schedule's owner and Super Admin")},
	"ScheduledReport":      {"recipient_email": pdField("contact", "Deliver the scheduled report", BasisEmployment, retainUser, "the schedule's owner and Super Admin")},
	"PIMExportSchedule":    {"recipient_email": pdField("contact", "Deliver the scheduled export", BasisEmployment, retainUser, "the schedule's owner and Super Admin")},
	"PasswordResetRequest": {"target_username": pdField("identity", "Whose password a dual-control reset is for", BasisEmployment, retainUser, readersAdmin)},

	// --- Suppliers (a sole proprietor's details are personal data) --------
	"Vendor": {
		"address":             pdField("address", "Supplier address on purchase orders and GST records", BasisVoluntary, retainVendor, readersFinance),
		"address_line2":       pdField("address", "Supplier address on purchase orders and GST records", BasisVoluntary, retainVendor, readersFinance),
		"city":                pdField("address", "Supplier address", BasisVoluntary, retainVendor, readersFinance),
		"pincode":             pdField("address", "Supplier address", BasisVoluntary, retainVendor, readersFinance),
		"contact_phone":       pdField("contact", "Reach the supplier about orders", BasisVoluntary, retainVendor, readersFinance),
		"contact_email":       pdField("contact", "Send purchase orders", BasisVoluntary, retainVendor, readersFinance),
		"gstin":               pdField("identity", "Supplier GSTIN for input-tax credit", BasisStatutory, retainVendor, readersFinance),
		"bank_account_number": pdField("financial", "Pay the supplier", BasisVoluntary, retainVendor, readersAdmin),
		"bank_ifsc":           pdField("financial", "Pay the supplier", BasisVoluntary, retainVendor, readersAdmin),
	},

	// --- The business's own data: listed so the decision is written down --
	"BankAccount": {
		"account_number": businessField("The company's own bank account"),
		"bank_name":      businessField("The company's own bank account"),
		"ifsc_code":      businessField("The company's own bank account"),
	},
	"BankStatementLine":  {"bank_account": businessField("Which company account a statement line belongs to")},
	"CourierServiceArea": {"pincode_prefix": businessField("Areas a courier serves")},
	"LegalEntity": {
		"address":       businessField("Registered address on invoices"),
		"address_line2": businessField("Registered address on invoices"),
		"city":          businessField("Registered address on invoices"),
		"pincode":       businessField("Registered address on invoices"),
		"contact_phone": businessField("Company contact on invoices"),
		"email":         businessField("Company contact on invoices"),
		"gstin":         businessField("Seller GSTIN on invoices"),
		"bank_account":  businessField("Company bank details printed on invoices"),
		"bank_ifsc":     businessField("Company bank details printed on invoices"),
		"bank_name":     businessField("Company bank details printed on invoices"),
	},
	"Location": {
		"address":       businessField("Store / warehouse address"),
		"address_line2": businessField("Store / warehouse address"),
		"city":          businessField("Store / warehouse address"),
		"pincode":       businessField("Store / warehouse address; courier pickup"),
		"contact_phone": businessField("Store contact"),
		"email":         businessField("Store contact"),
	},
}

// personalDataKinds and personalDataBases are the allowed vocabularies.
var (
	personalDataKinds = map[string]bool{"identity": true, "contact": true, "address": true, "demographic": true, "preference": true,
		"financial": true, "consent": true, "commercial": true, "record": true, "business": true}
	personalDataBases = map[string]bool{BasisConsent: true, BasisVoluntary: true, BasisEmployment: true, BasisStatutory: true, BasisNotPersonal: true}
)

// IsPersonalDataField reports whether the register must carry a row for
// doctype.field: a field of a person doctype, or a personal-looking name.
func IsPersonalDataField(doctype, field string) bool {
	return personDoctypes[doctype] || personalDataFieldPattern.MatchString(field)
}

// LookupPersonalDataField returns the register row for doctype.field.
func LookupPersonalDataField(doctype, field string) (PersonalDataField, bool) {
	f, ok := personalDataRegister[doctype][field]
	return f, ok
}

// PersonalDataReviewQuestions lists every row carrying a minimization
// question, as "Doctype.field: question", sorted.
func PersonalDataReviewQuestions() []string {
	var out []string
	for dt, fields := range personalDataRegister {
		for f, row := range fields {
			if row.Review != "" {
				out = append(out, dt+"."+f+": "+row.Review)
			}
		}
	}
	sort.Strings(out)
	return out
}
