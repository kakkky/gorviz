package assets

type Controller struct {
	Name string
	Path string
}

var GMPReplayController = Controller{
	Name: "gmp-replay",
	Path: "/assets/js/gmp_replay_controller.js",
}
