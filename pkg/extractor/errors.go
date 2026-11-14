package extractor

import "errors"

var (
	// ErrNotMotionPhoto is returned when no valid embedded video could be
	// located in the data.
	ErrNotMotionPhoto = errors.New("not a motion photo")

	// ErrUnsupportedExtension is returned by ExtractFile for files whose
	// extension is not accepted by SupportedExtension.
	ErrUnsupportedExtension = errors.New("unsupported file extension")

	// ErrNothingToExtract is returned by ExtractFile when both the photo and
	// the video are skipped.
	ErrNothingToExtract = errors.New("nothing to extract: both photo and video extraction are disabled")
)
