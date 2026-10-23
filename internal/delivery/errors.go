package delivery

import "fmt"

// DeliveryStatusUnknownError reports that DATA was fully transmitted but the
// final server response could not be read. Retrying may duplicate delivery.
type DeliveryStatusUnknownError struct{ Err error }

func (e *DeliveryStatusUnknownError) Error() string {
	return "SMTP delivery status unknown after DATA: " + e.Err.Error()
}
func (e *DeliveryStatusUnknownError) Unwrap() error { return e.Err }

type SMTPResponseError struct {
	Stage          SMTPStage
	EnhancedStatus string
	Code           int
	Line           string
}

func (e *SMTPResponseError) Error() string {
	return fmt.Sprintf("smtp response code=%d line=%q", e.Code, e.Line)
}

func (e *SMTPResponseError) Temporary() bool {
	return e.Code >= 400 && e.Code <= 499
}

func (e *SMTPResponseError) Permanent() bool {
	return e.Code >= 500 && e.Code <= 599
}
