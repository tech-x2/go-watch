package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pkg/exec"
	"github.com/radovskyb/watcher"
	"github.com/urfave/cli/v2"
)

var (
	Version  = "0.0.0"
	Revision = "0"
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("watch: ")
	log.SetOutput(os.Stderr)

	cmd := &cli.App{
		Name:      "watch",
		UsageText: "watch [--interval] [--target-exts] [--target-dirs] [--excludes] [--verbose] [command]",
		Version:   fmt.Sprintf("v%s.%s", Version, Revision),
		Flags: []cli.Flag{
			&cli.IntFlag{
				Name:    "interval",
				Aliases: []string{"i"},
				Value:   5,
				Usage:   "Polling interval",
			},
			&cli.StringSliceFlag{
				Name:    "target-exts",
				Aliases: []string{"t"},
				Value:   cli.NewStringSlice(".go"),
				Usage:   "Watch target exts",
			},
			&cli.StringSliceFlag{
				Name:  "target-dirs",
				Value: cli.NewStringSlice(),
				Usage: "Additional target dirs",
			},
			&cli.StringSliceFlag{
				Name:    "excludes",
				Aliases: []string{"e"},
				Value:   cli.NewStringSlice(),
				Usage:   "Exclude files",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Value: false,
				Usage: "Show verbose logs",
			},
		},
		Action: func(ctx *cli.Context) error {
			var (
				varboseFlag  = ctx.Bool("verbose")
				interval     = ctx.Int("interval")
				targetExts   = ctx.StringSlice("target-exts")
				targetDirs   = ctx.StringSlice("target-dirs")
				excludeFiles = ctx.StringSlice("excludes")
				cmds         = ctx.Args().Slice()
			)

			varbose := func(args ...interface{}) {
				if varboseFlag {
					log.Print(args...)
				}
			}

			if len(cmds) == 0 {
				cli.ShowAppHelpAndExit(ctx, 1)
			}

			w := watcher.New()
			{
				defer w.Close()

				ch := make(chan struct{}, 1)

				go func() {
					for {
						select {
						case ev := <-w.Event:
							switch ev.Op {
							case watcher.Chmod:
							default:
								varbose("received event: ", ev)

								ch <- struct{}{}
							}
						case err := <-w.Error:
							switch err {
							case watcher.ErrWatchedFileDeleted:
							default:
								log.Fatal(err)
							}
						case <-w.Closed:
							return
						}
					}
				}()

				go func() {
					targetDirs = append(targetDirs, ".")

					for {
						watchedFiles := w.WatchedFiles()

						for _, dir := range targetDirs {
							dir := dir

							if err := filepath.Walk(dir, func(p string, i os.FileInfo, err error) error {
								if err != nil {
									return err
								}

								if i.IsDir() {
									return nil
								}

								if m := func() bool {
									for _, file := range excludeFiles {
										if p == filepath.Clean(file) {
											return true
										}
									}

									return false
								}(); m {
									return nil
								}

								if m := func() bool {
									ext := filepath.Ext(p)

									for _, t := range targetExts {
										if ext == t {
											return true
										}
									}

									return false
								}(); m {
									file, err := filepath.Abs(p)

									if err != nil {
										return err
									}

									if _, ex := watchedFiles[file]; ex {
										return nil
									}

									if err := w.Add(file); err != nil {
										return err
									}

									log.Print("watching file: ", file)
								}

								return nil
							}); err != nil {
								log.Fatal(err)
							}
						}

						time.Sleep(time.Duration(interval) * time.Second)
					}
				}()

				go func() {
					for {
						cmd := exec.Command(cmds[0], cmds[1:]...)
						cmd.SysProcAttr = &syscall.SysProcAttr{
							Setpgid: true,
						}

						if err := cmd.Start(
							exec.Stdout(os.Stderr),
							exec.Stderr(os.Stderr),
						); err != nil {
							log.Fatal(err)
						}

						sig := make(chan os.Signal)

						signal.Notify(sig, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

						select {
						case <-sig:
							syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

							os.Exit(0)
						case <-ch:
							syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
						}
					}
				}()
			}

			return w.Start(time.Duration(interval) * time.Second)
		},
	}

	if err := cmd.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
