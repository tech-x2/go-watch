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
	flag "github.com/spf13/pflag"
)

var (
	Version  = "unset"
	Revision = "unset"
)

const usage = `usage:
  watch [--verbose] [--interval] [--targets] [--excludes] [command]

options:`

func main() {
	log.SetFlags(0)
	log.SetPrefix("watch: ")
	log.SetOutput(os.Stderr)

	flag.Usage = func() {
		fmt.Println(usage)
		flag.PrintDefaults()
	}

	var (
		versionFlag  bool
		helpFlag     bool
		varboseFlag  bool
		interval     uint
		targetExts   []string
		excludeFiles []string
	)

	flag.BoolVar(&versionFlag, "version", false, "show version")
	flag.BoolVarP(&helpFlag, "help", "h", false, "show help")
	flag.BoolVarP(&varboseFlag, "verbose", "v", false, "show varbose logs")
	flag.UintVarP(&interval, "interval", "i", 5, "polling interval")
	flag.StringSliceVarP(&targetExts, "targets", "t", []string{".go"}, "watch target exts")
	flag.StringSliceVarP(&excludeFiles, "excludes", "e", []string{}, "exclude files")
	flag.Parse()

	cmds := flag.Args()

	if versionFlag {
		fmt.Println("watch version:", Version)

		return
	}

	if helpFlag || len(cmds) == 0 {
		flag.Usage()

		return
	}

	varbose := func(args ...interface{}) {
		if varboseFlag {
			log.Print(args...)
		}
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
			for {
				watchedFiles := w.WatchedFiles()

				if err := filepath.Walk(".", func(p string, i os.FileInfo, err error) error {
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

	if err := w.Start(time.Duration(interval) * time.Second); err != nil {
		log.Fatal(err)
	}
}
