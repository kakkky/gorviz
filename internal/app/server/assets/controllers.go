package assets

// Controller は Stimulus の controller。Name は data-controller に書く名前、Path は書き出した JS の URL
type Controller struct {
	Name string
	Path string
}

// TimeRangePickerController は期間を選ぶ部品を外の操作で閉じる
var TimeRangePickerController = Controller{
	Name: "time-range-picker",
	Path: URLPrefix + "js/controllers/time_range_picker_controller.js",
}

// RowLinkController は一覧の行のどこを押しても、その行の先 (data-href) を開く
var RowLinkController = Controller{
	Name: "row-link",
	Path: URLPrefix + "js/controllers/row_link_controller.js",
}

// RowDetailController は一覧の行の印で、すぐ下の詳しい行を開け閉めする
var RowDetailController = Controller{
	Name: "row-detail",
	Path: URLPrefix + "js/controllers/row_detail_controller.js",
}

// ChartZoomController はグラフをドラッグして選んだ範囲を、画面の期間にして開き直す
var ChartZoomController = Controller{
	Name: "chart-zoom",
	Path: URLPrefix + "js/controllers/chart_zoom_controller.js",
}

// Controllers は layout.Base で読み込むすべての controller
var Controllers = []Controller{TimeRangePickerController, RowLinkController, RowDetailController, ChartZoomController}

// Island は turbo-mount で載せる Preact のアイランド。Component は islands.ts で登録した名前、Controller はその controller の名前
type Island struct {
	Component  string
	Controller string
}

// IslandsPath はアイランドを登録する module。stimulus.ScriptLoad の後に読む
const IslandsPath = URLPrefix + "js/islands.js"

var GMPReplayIsland = Island{Component: "GmpReplay", Controller: "turbo-mount-gmp-replay"}
