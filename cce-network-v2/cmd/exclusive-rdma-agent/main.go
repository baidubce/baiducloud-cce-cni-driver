package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging/logfields"
	"github.com/fsnotify/fsnotify"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// udsSocketFileName is the unix domain socket file name
	udsSocketFilePath = "/var/run/exclusive-rdma/exclusive-rdma-cni-plugin.sock"
)

type Plugin struct {
	PluginType string `json:"type"`
	DriverName string `json:"driver"`
}

type handler struct {
	podName string
	podNs   string
}

var (
	logger           = logging.DefaultLogger.WithField(logfields.LogSubsys, "exclusive-rdma-agent")
	conflistFilePath string
	resourceType     string // resource type used to judge if the pod has rdma requests, it shuld be the same as the one in pod yaml
	driverName       string // driver name used to identify the RDMA network device
)

func init() {

	flag.StringVar(&conflistFilePath, "config", "", "path to the conflist file together with its name")
	flag.StringVar(&resourceType, "rstype", "rdma", "set the resource type used to announce that pod needs rdma")
	flag.StringVar(&driverName, "driver", "", "Set the driver name used to identify the RDMA network device")
	flag.Parse()

	if conflistFilePath == "" {
		flag.Usage()
		logger.Error("conflist file path is not set")
		os.Exit(1)
	}
	if driverName == "" {
		flag.Usage()
		logger.Error("driver name is not set")
		os.Exit(1)
	}
}

func main() {
	// agent start
	logger.Info("exclusive-rdma-agent is starting!")
	run()
}

// we use two goroutine to run the agent
func run() {
	// 1. watch conflist file
	// wait to make sure conflist file exist, so as to modify the conflist file later than previous agent when agents starting up
	for {
		_, err := os.Stat(conflistFilePath)
		if os.IsNotExist(err) {
			logger.Info("conflist file not exist, wait 1s to retry")
			time.Sleep(1 * time.Second)
		} else if err != nil {
			logger.Errorf("stat conflist file error: %s", err.Error())
			os.Exit(1)
		} else {
			break
		}
	}
	// watch if conflist file is changed, add the fields we need back to it
	logger.Infof("start to watch conflist file: %s", conflistFilePath)
	os.Chmod(conflistFilePath, 0666)
	go watchConflistFile()

	// 2. http uds server
	if _, err := os.Stat(udsSocketFilePath); err == nil {
		if err := os.Remove(udsSocketFilePath); err != nil {
			logger.Errorf("remove uds socket file error: %s", err.Error())
			os.Exit(1)
		}
	}
	unixListener, err := net.Listen("unix", udsSocketFilePath)
	if err != nil {
		logger.Errorf("failed to listen uds socket file: %s", err.Error())
		os.Exit(1)
	}
	defer unixListener.Close()

	if err != nil {
		logger.Errorf("chmod uds socket file error: %s", err.Error())
		os.Exit(1)
	}

	// register responding function to handle http request and give response
	http.HandleFunc("/", httpHandleRequest)
	// start http server in another goroutine
	logger.Infof("Starting HTTP server using uds socket file: %s", udsSocketFilePath)
	if err := http.Serve(unixListener, nil); err != nil {
		logger.Errorf("failed to serve: %s", err.Error())
	}
}

// continue to watch conflist file and update it if necessary
func watchConflistFile() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorf("failed to create watcher: %v", err)
		os.Exit(1)
	}
	defer watcher.Close()

	err = watcher.Add(conflistFilePath)
	if err != nil {
		logger.Errorf("failed to watch file: %v", err)
		os.Exit(1)
	}
	// initial check and update, this is necessary when the agent starts up for the first time
	err = checkUpAndUpdateJSON(conflistFilePath)
	if err != nil {
		logger.Errorf("check and update json error: %s", err)
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				logger.Error("watcher closed unexpectedly")
				os.Exit(1)
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				logger.Infof("conflist file has been changed, start to update it")
				err = checkUpAndUpdateJSON(conflistFilePath)
				if err != nil {
					logger.Errorf("check and update json error: %s", err)
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				logger.Error("watcher closed unexpectedly")
				os.Exit(1)
			}
			logger.Errorf("watcher error: %s", err)
		}
	}
}

func checkUpAndUpdateJSON(filePath string) error {
	file, err := os.OpenFile(filePath, os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("open file error: %s", err.Error())
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return fmt.Errorf("read file error: %s", err.Error())
	}

	var conflist map[string]any
	err = json.Unmarshal(data, &conflist)
	if err != nil {
		return fmt.Errorf("unmarshal json error: %s", err.Error())
	}
	logger.Infof("checkup and update json: %s opened successfully, start to check and update it", filePath)

	found := false
	plugins, ok := conflist["plugins"].([]any)
	if !ok {
		return errors.New("plugins not found in conflist")
	}
	for _, item := range plugins {
		if plugin, ok := item.(map[string]any); !ok {
			logger.Error("wrong format: there is an item that is not a map, which cannot be recognized")
			continue
		} else if plugin["type"] == "exclusive-rdma" {
			found = true
			break
		}
	}

	if !found {
		logger.Info("exclusive-rdma plugin not found in conflist, start to add ")
		newPlugin := Plugin{
			PluginType: "exclusive-rdma",
			DriverName: driverName}
		conflist["plugins"] = append(plugins, newPlugin)

		updatedata, err := json.MarshalIndent(conflist, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal json error when writing back changes to file: %s", err.Error())
		}
		if err := os.WriteFile(filePath, updatedata, 0644); err != nil {
			return fmt.Errorf("write file error: %s", err.Error())
		} else {
			logger.Info("add exclusive-rdma plugin successfully, together with its driver name")
		}
	} else {
		logger.Info("checkup and update json: exclusive-rdma plugin exists in conflist")
	}

	return nil
}

func httpHandleRequest(w http.ResponseWriter, r *http.Request) {
	logger.Info("http request received: check if pod has rdma request, start to handle it")
	var handler = newHandler(
		r.URL.Query().Get("podName"),
		r.URL.Query().Get("podNs"))

	result, err := handler.handleRequest()
	if err != nil {
		logger.Errorf("handle request error: %s", err.Error())
		fmt.Fprintf(w, "handle request error: %s", err.Error())
	} else if result {
		w.Write([]byte("true"))
	} else {
		w.Write([]byte("false"))
	}
	logger.Info("http request handled and response sent")
}

func newHandler(podName string, podNs string) *handler {
	return &handler{
		podName: podName,
		podNs:   podNs,
	}
}

func (h *handler) handleRequest() (bool, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return false, fmt.Errorf("get in cluster config error: %s", err.Error())
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return false, fmt.Errorf("build in cluster client error: %s", err.Error())
	}

	pod, err := client.CoreV1().Pods(h.podNs).Get(context.TODO(), h.podName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("get pod: %s in namespace: %s error: %s", h.podName, h.podNs, err.Error())
	}

	for _, container := range pod.Spec.Containers {
		if hasRDMARequest(container.Resources.Limits) || hasRDMARequest(container.Resources.Requests) {
			return true, nil
		}
	}

	return false, nil
}

func hasRDMARequest(rl v1.ResourceList) bool {
	for key := range rl {
		arr := strings.Split(string(key), "/")
		if len(arr) != 2 {
			continue
		}
		if arr[0] == resourceType {
			return true
		}
	}
	return false
}
