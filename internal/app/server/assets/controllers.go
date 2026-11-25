package assets

// Controller は Stimulus の controller。Name は data-controller に書く名前、Path は書き出した JS の URL
type Controller struct {
	Name string
	Path string
}

// Controllers は layout.Base で読み込むすべての controller
var Controllers = []Controller{}

// Island は turbo-mount で載せる Preact のアイランド。Component は islands.ts で登録した名前、Controller はその controller の名前
type Island struct {
	Component  string
	Controller string
}

// IslandsPath はアイランドを登録する module。stimulus.ScriptLoad の後に読む
const IslandsPath = URLPrefix + "js/islands.js"

var GMPReplayIsland = Island{Component: "GmpReplay", Controller: "turbo-mount-gmp-replay"}
