package gemini

type GmiError struct {
	Code int
	err  error
}

func Error(code int, err error) error {
	return &GmiError{Code: code, err: err}
}

func (e *GmiError) Error() string {
	return e.err.Error()
}

func (e *GmiError) Unwrap() error {
	return e.err
}
