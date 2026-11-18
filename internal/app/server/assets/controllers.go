package assets

type Controller struct {
	Name string
	Path string
}

var GMPReplayController = Controller{
	Name: "gmp-replay",
	Path: "/assets/js/gmp_replay_controller.js",
}

var ChartSyncController = Controller{
	Name: "chart-sync",
	Path: "/assets/js/chart_sync_controller.js",
}

// KeepScrollController は data-keep-scroll のリンクで移ったときに、スクロール位置を保つ
var KeepScrollController = Controller{
	Name: "keep-scroll",
	Path: "/assets/js/keep_scroll_controller.js",
}

// TimeRangePickerController は期間を選ぶ部品を、外を押したときと Esc で閉じる
var TimeRangePickerController = Controller{
	Name: "time-range-picker",
	Path: "/assets/js/time_range_picker_controller.js",
}

// RowDetailController は一覧の行の印で、その行の詳しい行を開け閉めする
var RowDetailController = Controller{
	Name: "row-detail",
	Path: "/assets/js/row_detail_controller.js",
}

// RowLinkController は一覧の行をクリックで開けるようにする
var RowLinkController = Controller{
	Name: "row-link",
	Path: "/assets/js/row_link_controller.js",
}
