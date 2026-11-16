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
