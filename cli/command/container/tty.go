package container

import (
	"context"
	"fmt"
	"os"
	gosignal "os/signal"
	"runtime"
	"time"

	"github.com/docker/cli/cli/command"
	"github.com/moby/moby/client"
	"github.com/moby/sys/signal"
	"github.com/sirupsen/logrus"
)

// TODO(thaJeztah): split resizeTTYTo
type resizeClient interface {
	client.ExecAPIClient
	client.ContainerAPIClient
}

// resizeTTYTo resizes TTY to specific height and width.
func resizeTTYTo(ctx context.Context, apiClient resizeClient, id string, height, width uint, isExec bool) error {
	if height == 0 && width == 0 {
		return nil
	}

	var err error
	if isExec {
		_, err = apiClient.ExecResize(ctx, id, client.ExecResizeOptions{
			Height: height,
			Width:  width,
		})
	} else {
		_, err = apiClient.ContainerResize(ctx, id, client.ContainerResizeOptions{
			Height: height,
			Width:  width,
		})
	}

	if err != nil {
		logrus.Debugf("Error resize: %s\r", err)
	}
	return err
}

// resizeTty is to resize the tty with cli out's tty size
func resizeTty(ctx context.Context, cli command.Cli, id string, isExec bool) error {
	height, width := cli.Out().GetTtySize()
	return resizeTTYTo(ctx, cli.Client(), id, height, width, isExec)
}

// initTtySize is to init the TTYs size to the same as the window, if there is an error, it will retry 10 times.
func initTtySize(ctx context.Context, cli command.Cli, id string, isExec bool, resizeTtyFunc func(ctx context.Context, cli command.Cli, id string, isExec bool) error) {
	resizeTTYFunction := resizeTtyFunc
	if resizeTTYFunction == nil {
		resizeTTYFunction = resizeTty
	}
	if err := resizeTTYFunction(ctx, cli, id, isExec); err != nil {
		go func() {
			var err error
			for retryCount := 0; retryCount < 10; retryCount++ {
				time.Sleep(time.Duration(retryCount+1) * 10 * time.Millisecond)
				if err = resizeTTYFunction(ctx, cli, id, isExec); err == nil {
					break
				}
			}
			if err != nil {
				_, _ = fmt.Fprintln(cli.Err(), "failed to resize tty, using default size")
			}
		}()
	}
}

// MonitorTtySize updates the container tty size when the terminal tty changes size
func MonitorTtySize(ctx context.Context, cli command.Cli, id string, isExec bool) error {
	initTtySize(ctx, cli, id, isExec, resizeTty)
	if runtime.GOOS == "windows" {
		go func() {
			previousHeight, previousWidth := cli.Out().GetTtySize()
			for {
				time.Sleep(time.Millisecond * 250)
				height, width := cli.Out().GetTtySize()

				if previousWidth != width || previousHeight != height {
					_ = resizeTty(ctx, cli, id, isExec)
				}
				previousHeight = height
				previousWidth = width
			}
		}()
	} else {
		signalChannel := make(chan os.Signal, 1)
		gosignal.Notify(signalChannel, signal.SIGWINCH)
		go func() {
			for range signalChannel {
				_ = resizeTty(ctx, cli, id, isExec)
			}
		}()
	}
	return nil
}
