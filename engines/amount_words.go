package engines

import (
	"fmt"
	"math"
	"strings"
)

// Amount in words (Stage 40.1).
//
// A printed purchase order carries the payable in words as well as figures -
// it is what makes a tampered figure obvious, and Indian vendors expect it on
// the document. Written in the Indian numbering system (thousand / lakh /
// crore), not the short scale, because that is the system every other rupee
// figure in this product is grouped and read in.
//
// Lives in its own file rather than inside purchase_order.go because the
// sales invoice print sheet wants exactly the same string, and a second
// implementation of this is precisely how two documents for the same order
// end up disagreeing.

var wordsUnder20 = [...]string{
	"Zero", "One", "Two", "Three", "Four", "Five", "Six", "Seven", "Eight", "Nine",
	"Ten", "Eleven", "Twelve", "Thirteen", "Fourteen", "Fifteen", "Sixteen",
	"Seventeen", "Eighteen", "Nineteen",
}

var wordsTens = [...]string{
	"", "", "Twenty", "Thirty", "Forty", "Fifty", "Sixty", "Seventy", "Eighty", "Ninety",
}

// twoDigitWords renders 0-99. Callers never pass 0 except for the whole-amount
// zero case, which AmountInWords handles itself.
func twoDigitWords(n int) string {
	if n < 20 {
		return wordsUnder20[n]
	}
	if n%10 == 0 {
		return wordsTens[n/10]
	}
	return wordsTens[n/10] + " " + wordsUnder20[n%10]
}

// threeDigitWords renders 0-999.
func threeDigitWords(n int) string {
	if n < 100 {
		return twoDigitWords(n)
	}
	out := wordsUnder20[n/100] + " Hundred"
	if n%100 != 0 {
		out += " " + twoDigitWords(n%100)
	}
	return out
}

// integerWords renders a non-negative whole number in the Indian system:
// the last three digits, then two-digit groups for thousand, lakh and crore.
// Anything at or above one hundred crore keeps counting in crore
// ("One Hundred Twenty Crore"), which is how it is read aloud.
func integerWords(n int64) string {
	if n == 0 {
		return "Zero"
	}
	var parts []string
	if crore := n / 10000000; crore > 0 {
		parts = append(parts, integerWordsBelowCrore(crore)+" Crore")
		n %= 10000000
	}
	if lakh := n / 100000; lakh > 0 {
		parts = append(parts, twoDigitWords(int(lakh))+" Lakh")
		n %= 100000
	}
	if thousand := n / 1000; thousand > 0 {
		parts = append(parts, twoDigitWords(int(thousand))+" Thousand")
		n %= 1000
	}
	if n > 0 {
		parts = append(parts, threeDigitWords(int(n)))
	}
	return strings.Join(parts, " ")
}

// integerWordsBelowCrore renders the crore count itself, which can exceed 99
// on a large enough figure - so it recurses rather than assuming two digits.
func integerWordsBelowCrore(n int64) string {
	if n < 1000 {
		return threeDigitWords(int(n))
	}
	return integerWords(n)
}

// AmountInWords renders a rupee amount as it is written on an Indian tax
// document: "Rupees One Lakh Twenty Thousand and Fifty Paise Only".
//
// Rounds to the paise before splitting, so a float that is really 1234.565
// cannot print as "Fifty Six Paise" while the figures column says 1234.57.
// A negative amount is rendered with a leading "Minus" rather than rejected -
// a debit note legitimately carries one, and a print sheet that silently
// dropped the sign would be worse than one that states it.
func AmountInWords(amount float64) string {
	if math.IsNaN(amount) || math.IsInf(amount, 0) {
		return ""
	}
	sign := ""
	if amount < 0 {
		sign, amount = "Minus ", -amount
	}

	total := int64(math.Round(amount * 100))
	rupees, paise := total/100, total%100

	out := sign + "Rupees " + integerWords(rupees)
	if paise > 0 {
		out += " and " + twoDigitWords(int(paise)) + " Paise"
	}
	return out + " Only"
}

// FormatIndianCurrency groups a rupee figure the Indian way (12,34,567.89)
// rather than the 1,234,567.89 that a plain thousands separator produces.
// Used by the printed PO so the figures column matches the words column's
// numbering system.
func FormatIndianCurrency(amount float64) string {
	sign := ""
	if amount < 0 {
		sign, amount = "-", -amount
	}
	whole := fmt.Sprintf("%.0f", math.Floor(amount))
	decimals := fmt.Sprintf("%.2f", amount-math.Floor(amount))[2:]

	if len(whole) <= 3 {
		return sign + whole + "." + decimals
	}
	// Last three digits stand alone; everything before them is grouped in twos.
	head, tail := whole[:len(whole)-3], whole[len(whole)-3:]
	var groups []string
	for len(head) > 2 {
		groups = append([]string{head[len(head)-2:]}, groups...)
		head = head[:len(head)-2]
	}
	if head != "" {
		groups = append([]string{head}, groups...)
	}
	return sign + strings.Join(groups, ",") + "," + tail + "." + decimals
}
