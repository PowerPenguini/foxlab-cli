package topology

import "errors"

type ResultKind uint8

type ResultCode string

const (
	ResultError ResultKind = iota
	ResultInfo
	ResultSuccess
)

const (
	ResultCodeDiskNotAttached ResultCode = "disk-not-attached"
	ResultCodeDiskInfoInvalid ResultCode = "disk-info-invalid"
)

type Result struct {
	Message string
	Err     error
	Kind    ResultKind
	Code    ResultCode
	Changed bool
}

func Success(message string) Result {
	return Result{Message: message, Kind: ResultSuccess, Changed: true}
}

func Info(message string) Result {
	return Result{Message: message, Kind: ResultInfo}
}

func ChangedInfo(message string) Result {
	return Result{Message: message, Kind: ResultInfo, Changed: true}
}

func Failure(message string) Result {
	return Result{Message: message, Err: errors.New(message), Kind: ResultError}
}

func FailureWithCause(message string, err error) Result {
	if err == nil {
		return Failure(message)
	}
	return Result{Message: message, Err: err, Kind: ResultError}
}

func FailureWithCode(code ResultCode, message string) Result {
	result := Failure(message)
	result.Code = code
	return result
}

func (r Result) OK() bool {
	return r.Err == nil
}
