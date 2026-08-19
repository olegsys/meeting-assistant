package models

import "errors"

var (
	ErrNotFound                   = errors.New("resource not found")
	ErrAlreadyExists              = errors.New("resource already exists")
	ErrInvalidInput               = errors.New("invalid input")
	ErrAccessDenied               = errors.New("access denied")
	ErrNoMaterials                = errors.New("meeting has no materials yet")
	ErrTaskNotFailed              = errors.New("task is not failed")
	ErrUnsupported                = errors.New("unsupported provider")
	ErrEmptyTranscript            = errors.New("empty transcript")
	ErrEmptyFile                  = errors.New("empty file")
	ErrTranscriptionAlreadyExists = errors.New("transcription already exists")
	ErrSummaryAlreadyExists       = errors.New("summary already exists")
	ErrFileTooLarge               = errors.New("file too large")
)
