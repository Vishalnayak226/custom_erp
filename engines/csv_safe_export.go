package engines

import "io"

// SafeCSVWriter / NewSafeCSVWriter (Stage 49.5.7) let the server packages
// write a file people open in a spreadsheet with every cell made safe, as the
// import and export files here already are. A failed-rows file repeats cells
// from the uploaded workbook, so it must not run as a formula.
type SafeCSVWriter = safeCSVWriter

func NewSafeCSVWriter(w io.Writer) SafeCSVWriter { return newSafeCSVWriter(w) }
