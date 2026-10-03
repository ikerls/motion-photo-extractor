// Package extractor splits Samsung/Google motion photos into their still
// image and embedded video.
//
// It works at two levels:
//
//   - [Split] and [SanitizePhoto] operate on bytes in memory and never touch
//     the filesystem.
//   - [ExtractFile] reads a motion photo from disk and writes the extracted
//     components next to it (or into [Options.OutputDir]).
//
// The package has no dependencies outside the standard library and does not
// log; everything a caller may want to report is returned in [Result] or as
// an error that can be matched with [errors.Is].
package extractor
