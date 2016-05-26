package vmax

import "github.com/akutz/gofig"

const (
	// Name is the name of the storage driver
	Name = "vmax"
)

func init() {
	registerConfig()
}

func registerConfig() {
	r := gofig.NewRegistration("VMAX")
	r.Key(gofig.String, "", "", "", "vmax.endpoint")
	r.Key(gofig.Bool, "", true, "", "vmax.insecure")
	r.Key(gofig.Bool, "", false, "", "vmax.useCerts")
	r.Key(gofig.String, "", "smc", "", "vmax.userName")
	r.Key(gofig.String, "", "smc", "", "vmax.password")
	r.Key(gofig.String, "", "", "", "vmax.systemID")
	gofig.Register(r)
}
