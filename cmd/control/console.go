package control

import (
	"fmt"
	"io/ioutil"
	"sort"
	"strings"

	"golang.org/x/net/context"

	"github.com/codegangsta/cli"
	composeConfig "github.com/docker/libcompose/config"
	"github.com/docker/libcompose/project/options"
	"github.com/rancher/os/cmd/control/service"
	"github.com/rancher/os/compose"
	"github.com/rancher/os/config"
	"github.com/rancher/os/log"
	"github.com/rancher/os/util"
	"github.com/rancher/os/util/network"
)

func consoleSubcommands() []cli.Command {
	return []cli.Command{
		{
			Name:   "switch",
			Usage:  "switch console without a reboot",
			Action: consoleSwitch,
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "force, f",
					Usage: "do not prompt for input",
				},
				cli.BoolFlag{
					Name:  "no-pull",
					Usage: "don't pull console image",
				},
			},
		},
		{
			Name:   "enable",
			Usage:  "set console to be switched on next reboot",
			Action: consoleEnable,
		},
		{
			Name:   "list",
			Usage:  "list available consoles",
			Action: consoleList,
		},
	}
}

func consoleSwitch(c *cli.Context) error {
	if len(c.Args()) != 1 {
		log.Fatal("Must specify exactly one console to switch to")
	}
	newConsole := c.Args()[0]

	cfg := config.LoadConfig()
	validateConsole(newConsole, cfg)
	if newConsole == currentConsole() {
		log.Warnf("Console is already set to %s", newConsole)
	}

	if !c.Bool("force") {
		fmt.Println(`Switching consoles will
1. destroy the current console container
2. log you out
3. restart Docker`)
		if !util.Yes("Continue") {
			return nil
		}
	}

	if !c.Bool("no-pull") && newConsole != "default" {
		if err := compose.StageServices(cfg, newConsole); err != nil {
			return err
		}
	}

	service, err := compose.CreateService(nil, "switch-console", &composeConfig.ServiceConfigV1{
		LogDriver:  "json-file",
		Privileged: true,
		Net:        "host",
		Pid:        "host",
		Image:      config.OsBase,
		Labels: map[string]string{
			config.ScopeLabel: config.System,
		},
		Command:     []string{"/usr/bin/ros", "switch-console", newConsole},
		VolumesFrom: []string{"all-volumes"},
	})
	if err != nil {
		return err
	}

	if err = service.Delete(context.Background(), options.Delete{}); err != nil {
		return err
	}
	if err = service.Up(context.Background(), options.Up{}); err != nil {
		return err
	}
	return service.Log(context.Background(), true)
}

func consoleEnable(c *cli.Context) error {
	if len(c.Args()) != 1 {
		log.Fatal("Must specify exactly one console to enable")
	}
	newConsole := c.Args()[0]

	cfg := config.LoadConfig()
	validateConsole(newConsole, cfg)

	//TODO: why does default not need to be staged?
	if newConsole != "default" {
		if err := compose.StageServices(cfg, newConsole); err != nil {
			return err
		}
	}

	if err := config.Set("rancher.console", newConsole); err != nil {
		log.Errorf("Failed to update 'rancher.console': %v", err)
	}

	return nil
}

func consoleList(c *cli.Context) error {
	cfg := config.LoadConfig()
	consoles := availableConsoles(cfg)
	currentConsole := currentConsole()

	for _, console := range consoles {
		if console == currentConsole {
			fmt.Printf("current  %s\n", console)
		} else if console == cfg.Rancher.Console {
			fmt.Printf("enabled  %s\n", console)
		} else {
			fmt.Printf("disabled %s\n", console)
		}
	}

	return nil
}

func validateConsole(console string, cfg *config.CloudConfig) {
	consoles := availableConsoles(cfg)
	if !service.IsLocalOrURL(console) && !util.Contains(consoles, console) {
		log.Fatalf("%s is not a valid console", console)
	}
}

func availableConsoles(cfg *config.CloudConfig) []string {
	consoles, err := network.GetConsoles(cfg.Rancher.Repositories.ToArray())
	if err != nil {
		log.Fatal(err)
	}
	consoles = append(consoles, "default")
	sort.Strings(consoles)
	return consoles
}

func currentConsole() (console string) {
	consoleBytes, err := ioutil.ReadFile("/run/console-done")
	if err == nil {
		console = strings.TrimSpace(string(consoleBytes))
	} else {
		log.Warnf("Failed to detect current console: %v", err)
	}
	return
}
