package delivery

import "fmt"

type SMTPResponseError struct {
	Code int
	Line string
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
