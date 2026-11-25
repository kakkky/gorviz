// Package docs は cli に埋め込むドキュメント。gorviz docs で引き、ヘルプとエラーに示す
package docs

import "embed"

//go:embed *.md
var FS embed.FS
