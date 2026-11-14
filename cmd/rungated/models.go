package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/amareshhebbar/rungated/internal/detect/cloud"
	"github.com/amareshhebbar/rungated/internal/models"
)

func modelsCmd(args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		fmt.Print(models.Table())
		fmt.Println()
		fmt.Println("free forever models run on your machine: rungated models pull ID")
		fmt.Println("trial models are hosted: ask hebbar.gvamaresh@gmail.com for a free key and set RUNGATED_CLOUD_API_KEY")
	case "pull":
		fs := flag.NewFlagSet("models pull", flag.ExitOnError)
		dir := fs.String("dir", "", "models directory (default "+models.DefaultDir()+")")
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: rungated models pull ID [--dir D]"))
		}
		id := args[1]
		fs.Parse(args[2:])
		p := &models.Puller{Token: os.Getenv("HF_TOKEN"), Log: os.Stderr}
		path, err := p.Pull(context.Background(), id, *dir)
		if err != nil {
			fatal(err)
		}
		fmt.Println(path)
	case "path":
		fs := flag.NewFlagSet("models path", flag.ExitOnError)
		dir := fs.String("dir", "", "models directory")
		if len(args) < 2 {
			fatal(fmt.Errorf("usage: rungated models path ID [--dir D]"))
		}
		id := args[1]
		fs.Parse(args[2:])
		f, err := models.ModelFile(*dir, id)
		if err != nil {
			fatal(err)
		}
		fmt.Println(f)
	case "status":
		fs := flag.NewFlagSet("models status", flag.ExitOnError)
		key := fs.String("api-key", os.Getenv("RUNGATED_CLOUD_API_KEY"), "Rungated cloud API key")
		url := fs.String("url", envOr("RUNGATED_CLOUD_URL", "https://api.rungated.dev"), "Rungated cloud URL")
		fs.Parse(args[1:])
		if *key == "" {
			fatal(fmt.Errorf("set --api-key or RUNGATED_CLOUD_API_KEY"))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		e, err := cloud.New(*url, *key, 0).Entitlements(ctx)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("plan %s\n\n%-22s %-10s %-8s %s\n", e.Plan, "MODEL", "TIER", "ACTIVE", "DAYS LEFT")
		for _, m := range e.Models {
			left := "unlimited"
			if m.DaysLeft >= 0 {
				left = fmt.Sprint(m.DaysLeft)
			}
			fmt.Printf("%-22s %-10s %-8v %s\n", m.ID, m.Tier, m.Active, left)
		}
	default:
		fatal(fmt.Errorf("unknown models command %q", args[0]))
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
