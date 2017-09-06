package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/openchirp/framework"
)

const (
	defaultFrameworkURI = "http://localhost:7000"
	defaultBrokerURI    = "tls://localhost:1883"
	defaultServiceID    = ""
	defaultServiceToken = ""
)

/* Options to be filled in by arguments */
var frameworkURI string
var brokerURI string
var serviceID string
var serviceToken string

/* HACK to allow us to use raw MQTT during conversion period */
var mqttQos int64

/* Setup argument flags and help prompt */
func init() {
	flag.StringVar(&frameworkURI, "framework", defaultFrameworkURI, "Sets the HTTP REST framework server URI")
	flag.StringVar(&brokerURI, "broker", defaultBrokerURI, "Sets the MQTT broker URI associated with the framework server")
	flag.StringVar(&serviceID, "id", defaultServiceID, "Sets the service ID associated with this service instance")
	flag.StringVar(&serviceToken, "token", defaultServiceToken, "Sets the service access token associated with this instance")
}

func main() {
	// var lorawanMQTTQos string
	// var lorawanMQTTBroker string
	// var lorawanMQTTUser string
	// var lorawanMQTTPass string

	flag.Parse()

	c, err := framework.StartServiceClient(frameworkURI, brokerURI, serviceID, serviceToken)
	if err != nil {
		fmt.Println("Error - ", err)
	}

	err = c.SetStatus("Started GW service")
	if err != nil {
		fmt.Println("Error - ", err)
	}

	updates, err := c.StartDeviceUpdates()
	if err != nil {
		fmt.Println("Error - ", err)
	}

	fmt.Println("# Starting to process device updates")

	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGINT)

	for {
		select {
		case update := <-updates:
			fmt.Println(update)
			c.SetDeviceStatus(update.Id, "Asked to reg gwid \"", update.Config["Gateway ID"], "\" at ", time.Now().Format(time.UnixDate))
		case <-signals:
			goto cleanup
		}
	}

cleanup:

	fmt.Println("# Shutting Down")
	c.StopDeviceUpdates()
	c.StopClient()
}
