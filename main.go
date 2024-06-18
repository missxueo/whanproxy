package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"

	"github.com/sirupsen/logrus"
)

type Proxy struct {
	Port     int
	Upstream string
	listener net.Listener
	logger   *logrus.Logger
}

func newProxy(port int, upstream string, logger *logrus.Logger) *Proxy {
	return &Proxy{
		Port:     port,
		Upstream: upstream,
		logger:   logger,
	}
}

func (p *Proxy) Listen() error {
	listener, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", p.Port))
	if err != nil {
		return err
	}
	p.listener = listener
	return nil
}

func (p *Proxy) Proxy(wait bool) {
	if wait {
		p.loopProxy(p.listener)
	} else {
		go p.loopProxy(p.listener)
	}
}

func (p *Proxy) loopProxy(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			p.logger.Panic(err)
		}
		go p.resolveConn(conn)
	}
}

func (p *Proxy) resolveConn(conn net.Conn) {
	p.logger.Infof("resolve %s from %s", conn.LocalAddr().String(), conn.RemoteAddr().String())
	upstream, err := net.Dial("tcp", fmt.Sprintf("%s:%d", p.Upstream, p.Port))
	if err != nil {
		conn.Close()
		p.logger.WithError(err).Warnf("connnect up %s:%d", p.Upstream, p.Port)
		return
	}
	defer upstream.Close()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go func() {
		_, err := io.Copy(upstream, conn)
		if err != nil {
			if err == io.EOF {
				p.logger.Infof("stream '%d' down -> up EOF", p.Port)
			} else {
				p.logger.Warnf("stream '%d' down -> up error: %v", p.Port, err)
			}
		}
		conn.Close()
		wg.Done()
	}()
	wg.Add(1)
	go func() {
		_, err = io.Copy(conn, upstream)
		if err != nil {
			if err == io.EOF {
				p.logger.Infof("stream '%d' up -> down EOF", p.Port)
			} else {
				p.logger.Warnf("stream '%d' up -> down error: %v", p.Port, err)
			}
		}
		wg.Done()
	}()
	wg.Wait()
	p.logger.Infof("stream %d done.", p.Port)
}

func newLogger() *logrus.Logger {
	logger := logrus.New()
	logger.Level = logrus.DebugLevel

	return logger
}

func main() {
	logger := newLogger()
	var uphost string
	flag.StringVar(&uphost, "uphost", "host.docker.internal", "upstream hostname")
	flag.Parse()
	args := flag.Args()
	var ports []int = make([]int, len(args))
	for i, arg := range args {
		port, err := strconv.Atoi(arg)
		if err != nil {
			logger.Panic(fmt.Errorf("port: %s is not a int", arg))
			return
		}
		ports[i] = port
	}
	if len(ports) == 0 {
		logger.Panic(errors.New("no ports"))
		return
	}
	wg := &sync.WaitGroup{}
	for _, port := range ports {
		proxy := newProxy(port, uphost, logger)
		if err := proxy.Listen(); err != nil {
			logger.Panicln(err)
			break
		}
		logger.Infof("proxy %d -> %s:%d", port, uphost, port)
		wg.Add(1)
		go func() {
			proxy.Proxy(true)
			wg.Done()
		}()
	}
	wg.Wait()
	logger.Infof("proxy stoped")
}
