// The order the loraserverMQTT arguments are picked up is as follows:
// 1) If lsbroker is specified on commandline, use all cmdline parameters
// 2) If lsbroker is specified as a service property, use all service props
// 3) If broker was unset in 1 or 2, set Broker, User, and Pass from framework args
package main

import (
	"flag"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/openchirp/framework"
	"github.com/sirupsen/logrus"
)

const (
	// Set this value to true to have the service publish a service status of
	// "Running" each time it receives a device update event
	//
	// This could be used as a service alive pulse if enabled
	// Otherwise, the service status will indicate "Started" at the time the
	// service "Started" the client
	runningStatus = true

	defaultFrameworkURI = "http://localhost:7000"
	defaultBrokerURI    = "tls://localhost:1883"
	defaultServiceID    = ""
	defaultServiceToken = ""

	defaultLsBroker = "<framework_broker>"
	defaultLsQoS    = "2"
	defaultLsUser   = "<framework_id>"
	defaultLsPass   = "<framework_pass>"
)

const (
	hexChars        = "0123456789abcdef"
	gatewayIdKey    = "Gateway ID"
	gatewayIdLength = len("D00D8BADF00D0001")
)

/* Options to be filled in by arguments */
var frameworkURI string
var brokerURI string
var serviceID string
var serviceToken string

var loraserverMQTTBroker string
var loraserverMQTTQoS string
var loraserverMQTTUser string
var loraserverMQTTPass string

func isHexCharacters(id string) bool {
	for _, c := range id {
		if !(('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')) {
			return false
		}
	}
	return true
}


/* Setup argument flags and help prompt */
func init() {
	/* Setup Arguments */
	flag.StringVar(&frameworkURI, "framework", defaultFrameworkURI, "Sets the HTTP REST framework server URI")
	flag.StringVar(&brokerURI, "broker", defaultBrokerURI, "Sets the MQTT broker URI associated with the framework server")
	flag.StringVar(&serviceID, "id", defaultServiceID, "Sets the service ID associated with this service instance")
	flag.StringVar(&serviceToken, "token", defaultServiceToken, "Sets the service access token associated with this instance")

	flag.StringVar(&loraserverMQTTBroker, "lsbroker", defaultLsBroker, "Sets the loraserver's MQTT broker")
	flag.StringVar(&loraserverMQTTQoS, "lsqos", defaultLsQoS, "Sets the loraserver's MQTT QoS")
	flag.StringVar(&loraserverMQTTUser, "lsuser", defaultLsUser, "Sets the loraserver's MQTT Username")
	flag.StringVar(&loraserverMQTTPass, "lspass", defaultLsPass, "Sets the loraserver's MQTT Password")
}

func main() {
	flag.Parse()

	/* Setup Logging */
	log := logrus.New()
	log.SetLevel(logrus.Level(5))

	log.Info("Starting LoRaWAN Gateways Service")

	c, err := framework.StartServiceClientStatus(frameworkURI, brokerURI, serviceID, serviceToken, "Unexpected disconnect!")
	if err != nil {
		log.Error("Failed to StartServiceClient: ", err)
		return
	}
	defer c.StopClient()
	log.Info("Started LoRaWAN Gateways Service")

	err = c.SetStatus("Starting")
	if err != nil {
		log.Error("Failed to publish service status: ", err)
		return
	}
	log.Info("Published Service Status")

	/* Hunt dow the loraserver MQTT parameters */
	if loraserverMQTTBroker == defaultLsBroker {
		// Try setting loraserverMQTT parameters from service properties
		loraserverMQTTBroker = c.GetProperty("MQTTBroker")
		loraserverMQTTQoS = c.GetProperty("MQTTQos")
		loraserverMQTTUser = c.GetProperty("MQTTUser")
		loraserverMQTTPass = c.GetProperty("MQTTPass")

		if loraserverMQTTBroker == "" {
			// Set to framework mqtt parameters
			loraserverMQTTBroker = brokerURI
			loraserverMQTTQoS = defaultLsQoS
			loraserverMQTTUser = serviceID
			loraserverMQTTPass = serviceToken
			logitem := log.WithFields(log.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
			logitem.Info("Used loraserver's MQTT broker parameters from framework broker settings")
		} else {
			// Using all service properties parameters
			logitem := log.WithFields(log.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
			logitem.Info("Used loraserver's MQTT broker parameters from service properties")
		}
	} else {
		// Using all commandline parameters
		if loraserverMQTTUser == defaultLsUser {
			loraserverMQTTUser = ""
		}
		if loraserverMQTTPass == defaultLsPass {
			loraserverMQTTPass = ""
		}
		logitem := log.WithFields(log.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
		logitem.Info("Used loraserver's MQTT broker parameters from commandline")
	}
	// Set QoS if not specified
	if loraserverMQTTQoS == "" {
		loraserverMQTTQoS = defaultLsQoS
	}

	/* Start the loraserver interface MQTT client */
	log.Info("Starting loraserver MQTT client")
	lsMQTT, err := NewMQTTClient(
		loraserverMQTTBroker,
		loraserverMQTTUser,
		loraserverMQTTPass,
		ParseMQTTQoS(loraserverMQTTQoS),
		false,
	)
	if err != nil {
		log.Error("Failed to start loraserver MQTT client: ", err)
		return
	}
	defer lsMQTT.Disconnect()

	/* Create the MQTT bridge manager */
	mqttBridge := NewBridgeService(c, lsMQTT)

	/* Create gateway id --> device id table */
	gwidDevice := make(map[string]string)
	deviceGwid := make(map[string]string)

	log.Info("Starting Device Updates Stream")
	updates, err := c.StartDeviceUpdates()
	if err != nil {
		log.Error("Failed to start device updates stream: ", err)
		return
	}
	defer c.StopDeviceUpdates()

	log.Info("Processing device updates")
	signals := make(chan os.Signal)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	err = c.SetStatus("Started")
	if err != nil {
		log.Error("Failed to publish service status: ", err)
		return
	}
	log.Info("Published Service Status")

	for {
		select {
		case update := <-updates:
			if runningStatus {
				err = c.SetStatus("Running")
				if err != nil {
					log.Error("Failed to publish service status: ", err)
					return
				}
				log.Info("Published Service Status")
			}

			logitem := log.WithFields(log.Fields{"type": update.Type, "devid": update.Id, "gwid": update.Config[gatewayIdKey]})

			switch update.Type {
			case framework.DeviceUpdateTypeRem:
				logitem.Info("Removing links")
				if _, ok := deviceGwid[update.Id]; ok {
					mqttBridge.RemoveLinksAll(update.Id)
					delete(gwidDevice, deviceGwid[update.Id])
					delete(deviceGwid, update.Id)
				}
			case framework.DeviceUpdateTypeUpd:
				logitem.Info("Removing links for update")
				if _, ok := deviceGwid[update.Id]; ok {
					mqttBridge.RemoveLinksAll(update.Id)
					delete(gwidDevice, deviceGwid[update.Id])
					delete(deviceGwid, update.Id)
				}
				fallthrough
			case framework.DeviceUpdateTypeAdd:
				logitem.Info("Adding links")
				gwid, ok := update.Config[gatewayIdKey]
				/* Check that the Gateway ID parameter was specified in the config */
				if !ok {
					logitem.Warn("No \"", gatewayIdKey, "\" key specified in config")
					c.SetDeviceStatus(update.Id, "No Gateway ID in config")
					continue
				}
				/* Check the length of the gateway id */
				if len(gwid) != gatewayIdLength {
					logitem.Warn("Gateway ID \"", gwid, "\" contains invalid hex characters")
					c.SetDeviceStatus(update.Id, "Gateway ID has invalid length (", len(gwid), "!=", gatewayIdLength, ")")
					continue
				}
				/* Check that all characters are valid hex characters */
				if !isHexCharacters(gwid) {
					logitem.Warn("Gateway ID \"", gwid, "\" contains non-hex characters")
					c.SetDeviceStatus(update.Id, "Gateway ID contains non-hex chars")
					continue
				}
				/* Change case to lowercase */
				gwid = strings.ToLower(gwid)

				/* Check that the gateway id is not already mapped */
				if devid, ok := gwidDevice[gwid]; ok {
					logitem.Warn("Gateway ID \"", gwid, "\" already owned by device id ", devid)
					c.SetDeviceStatus(update.Id, "Gateway ID already in use by device id ", devid)
					continue
				}
				gwidDevice[gwid] = update.Id
				deviceGwid[update.Id] = gwid

				c.SetDeviceStatus(update.Id, "Linking as gateway ", gwid)
				devTopic := "openchirp/devices/" + update.Id + "/transducer"
				lsTopic := "gateway/" + gwid
				logitem.Infof("Adding link %s --> %s", devTopic+"/rx", lsTopic+"/rx")
				mqttBridge.AddLinkFwd(update.Id, devTopic+"/rx", lsTopic+"/rx")
				logitem.Infof("Adding link %s --> %s", devTopic+"/status", lsTopic+"/status")
				mqttBridge.AddLinkFwd(update.Id, devTopic+"/stats", lsTopic+"/stats")
				logitem.Infof("Adding link %s <-- %s", devTopic+"/tx", lsTopic+"/tx")
				mqttBridge.AddLinkRev(update.Id, devTopic+"/tx", lsTopic+"/tx")
				c.SetDeviceStatus(update.Id, "Linked as gateway ", gwid)
			}
		case <-signals:
			goto cleanup
		}
	}

cleanup:
	log.Info("Shutting down")

	err = c.SetStatus("Shutting down")
	if err != nil {
		log.Error("Failed to publish service status: ", err)
		return
	}
	log.Info("Published Service Status")
}
