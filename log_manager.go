package main

import (
	"errors"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"sync"
	"time"

	"github.com/Senior-Design-May1601/projectmain/control"
	"github.com/Senior-Design-May1601/projectmain/loggerplugin"
)

const (
	READY_TIMEOUT = 3 // seconds
)

type connectionKey struct {
	Port int
	Name string
}

type loggerConnectionMap struct {
	sync.RWMutex
	values map[connectionKey]*rpc.Client
}

type LogManager struct {
	callChan          chan *rpc.Call
	manager           ProcessManager
	loggerConnections loggerConnectionMap
	listener          net.Listener
	readyChan         chan int
}

func NewLogManager(configs []PluginConfig) *LogManager {
	manager := LogManager{
		callChan: make(chan *rpc.Call, 100),
		manager:  *NewProcessManager(configs),
		loggerConnections: loggerConnectionMap{
			values: make(map[connectionKey]*rpc.Client),
		},
		listener:  nil,
		readyChan: make(chan int, 25),
	}

	rpc.Register(&manager)
	rpc.HandleHTTP()
	l, e := net.Listen("tcp", control.CONTROL_PORT_CORE)
	if e != nil {
		errExit(e.Error())
	}

	manager.listener = l
	go http.Serve(l, nil)
	go manager.handleCallReplies()

	log.Println("Log manager listening on:", control.CONTROL_PORT_CORE)

	return &manager
}

func (x *LogManager) StartLoggers() error {
	err := x.manager.StartProcesses()
	if err != nil {
		return err
	}

	for i := 0; i < x.manager.NumProcs(); i++ {
		select {
		case port := <-x.readyChan:
			log.Println("Logger ready on port", port)
		case <-time.After(time.Second * READY_TIMEOUT):
			return errors.New("Logger connection timeout.")
		}
	}

	close(x.readyChan)

	return nil
}

func (x *LogManager) StopLoggers() error {
	return x.manager.KillProcesses()
}

func (x *LogManager) Ready(arg loggerplugin.ReadyArg, _ *int) error {
	go x.connect(connectionKey{arg.Port, arg.Name})
	return nil
}

func (x *LogManager) Log(p []byte, _ *int) error {
	log.Println("Got log event:", string(p))
	x.loggerConnections.RLock()
	defer x.loggerConnections.RUnlock()
	var r int
	for key, client := range x.loggerConnections.values {
		client.Go(key.Name+".Log", p, &r, x.callChan)
	}

	return nil
}

func (x *LogManager) connect(key connectionKey) error {
	client, err := rpc.DialHTTP("tcp", "localhost:"+strconv.Itoa(key.Port))
	if err != nil {
		return err
	}

	x.loggerConnections.Lock()
	x.loggerConnections.values[key] = client
	x.loggerConnections.Unlock()

	x.readyChan <- key.Port

	return nil
}

func (x *LogManager) handleCallReplies() {
	for call := range x.callChan {
		if call.Error != nil {
			errExit(call.Error.Error())
		}
	}
}
