package repository

import "errors"

var ErrNotFound = errNotFound("resource not found")

var (
	ErrConflict  = errors.New("resource conflict")
	ErrLeaseLost = errors.New("task lease lost")
)

type errNotFound string

func (e errNotFound) Error() string { return string(e) }
