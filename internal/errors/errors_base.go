package errors

type ErrorType int

type CustomError struct {
	Type    ErrorType
	Message string
}

func (e *CustomError) Error() string {
	return e.Message
}

func NewCustomError(errType ErrorType, msg string) *CustomError {
	return &CustomError{
		Type:    errType,
		Message: msg,
	}
}
