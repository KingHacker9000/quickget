package agent

import "quickget/pkg/quickget/filename"

const defaultOutputFilename = filename.DefaultOutputFilename

func deriveSafeOutputFilenameFromURL(rawURL string) string {
	return filename.DeriveSafeOutputFilenameFromURL(rawURL)
}
