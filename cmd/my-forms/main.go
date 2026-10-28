package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/oneworkspacestudio/my-forms/internal/forms"
	"github.com/oneworkspacestudio/my-forms/web"
	"github.com/oneworkspacestudio/one-workspace/pkg/apphost"
	"net"
	"os"
	"os/signal"
	"path/filepath"
)

var version = "0.1.0-preview.1"

func run() error {
	data := flag.String("data", "", "App-local session/cache directory (NTFS)")
	listen := flag.String("listen", "127.0.0.1:0", "Loopback listener; zero selects an available port")
	origin := flag.String("origin", "", "Public application URL")
	_ = flag.Bool("no-browser", false, "Managed mode")
	v := flag.Bool("version", false, "Print version")
	reindex := flag.Bool("reindex", false, "Reconcile and integrity-check all Drive records, then exit")
	flag.Parse()
	if *v {
		fmt.Println(version)
		return nil
	}
	if *data == "" {
		root, e := os.UserConfigDir()
		if e != nil {
			return e
		}
		*data = filepath.Join(root, "OneWorkspace", "MyForms")
	}
	abs, e := filepath.Abs(*data)
	if e != nil {
		return e
	}
	if *listen == "127.0.0.1:0" {
		l, e := net.Listen("tcp", *listen)
		if e != nil {
			return e
		}
		*listen = l.Addr().String()
		l.Close()
	}
	if *origin == "" {
		*origin = "http://" + *listen
	}
	c := apphost.FromEnv(apphost.Config{ID: "forms", Name: "My Forms", Version: version, Data: abs, Listen: *listen, Origin: *origin})
	if c.Issuer == "" {
		return fmt.Errorf("Configure OneAccount through One Workspace before starting My Forms")
	}
	h, e := apphost.New(c, web.Files)
	if e != nil {
		return e
	}
	defer h.Close()
	drive, e := forms.NewDrive(os.Getenv("MYFORMS_DRIVE_URL"), os.Getenv("MYFORMS_DRIVE_TOKEN"))
	if e != nil {
		return e
	}
	a, e := forms.New(h, drive)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if *reindex {
		return a.Store.Reindex(ctx)
	}
	go a.Run(ctx)
	fmt.Println("My Forms: " + c.Origin)
	return h.Run(ctx)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
