//go:build windows

package main

import "golang.org/x/sys/windows/svc"

type centralService struct {
	run func(<-chan struct{}) error
}

func (s *centralService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}

	stop := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.run(stop)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				close(stop)
				<-errCh
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			}
		case err := <-errCh:
			if err != nil {
				return false, 1
			}
			changes <- svc.Status{State: svc.Stopped}
			return false, 0
		}
	}
}

func isWindowsService() (bool, error) {
	return svc.IsWindowsService()
}

func runWindowsService(name string, run func(<-chan struct{}) error) error {
	return svc.Run(name, &centralService{run: run})
}
