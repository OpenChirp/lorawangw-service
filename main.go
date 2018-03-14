// The order the loraserverMQTT arguments are picked up is as follows:
// 1) If lsbroker is specified on commandline, use all cmdline parameters
// 2) If lsbroker is specified as a service property, use all service props
// 3) If broker was unset in 1 or 2, set Broker, User, and Pass from framework args
package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/brocaar/loraserver/models"
	"github.com/brocaar/lorawan"
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

	// Set this to true to allow gateways to interact in the
	// <devid>/transducer/{tx, rx, stats} mqtt domain in addition to
	// the <devid>/gateway/<gwid>/{tx, rx, stats} prefix domain
	supportTransducerGateway = false

	// Set this to true to have the service decode RX packets and publish
	// on packets it receives
	supportRXPacketStats = true

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

const (
	topicLat       = "latitude"
	topicLon       = "longitude"
	topicAlt       = "altitude"
	topicPktRecv   = "packets_received"
	topicPktRecvOk = "packets_received_ok"
)

const (
	topicFrequency       = "rx_frequency"
	topicRSSI            = "rx_rssi"
	topicLoRaSNR         = "rx_lorasnr"
	topicSpreadingFactor = "rx_spreadingfactor"
	topicBandwidth       = "rx_bandwidth"
	topicDevAddr         = "rx_devaddr"
	topicNetworkID       = "rx_networkid"
	topicCRCStatus       = "rx_crcstatus"
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

/*
 {
	 "mac":"d00d8badf00d0003",
	 "time":"2017-09-11T02:16:14Z",
	 "latitude":40.44127,
	 "longitude":-79.94218,
	 "altitude":317,
	 "rxPacketsReceived":0,
	 "rxPacketsReceivedOK":0
	}
*/

type StatsPacket struct {
	Mac          string  `json:"mac"`
	Time         string  `json:"time"`
	Latitude     float64 `json:"Latitude"`
	Longitude    float64 `json:"Longitude"`
	Altitude     int     `json:"altitude"`
	RxReceived   uint    `json:"rxPacketsReceived"`
	RxReceivedOk uint    `json:"rxPacketsReceivedOK"`
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

	/* Hunt down the loraserver MQTT parameters */
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
			logitem := log.WithFields(logrus.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
			logitem.Info("Used loraserver's MQTT broker parameters from framework broker settings")
		} else {
			// Using all service properties parameters
			logitem := log.WithFields(logrus.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
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
		logitem := log.WithFields(logrus.Fields{"user": loraserverMQTTUser, "broker": loraserverMQTTBroker})
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
	mqttBridge := NewBridgeService(c, lsMQTT, log)

	/* Create gateway id --> device id table */
	gwidDevice := make(map[string]string)
	deviceGwid := make(map[string]string)

	log.Info("Starting Device Updates Stream")
	updates, err := c.StartDeviceUpdatesSimple()
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

			logitem := log.WithFields(logrus.Fields{"type": update.Type, "devid": update.Id, "gwid": update.Config[gatewayIdKey]})

			switch update.Type {
			case framework.DeviceUpdateTypeErr:
				log.Errorf("Event Error: %v", error(update))
			case framework.DeviceUpdateTypeRem:
				logitem.Info("Removing links")
				if gwid, ok := deviceGwid[update.Id]; ok {
					devTopic := "openchirp/devices/" + update.Id + "/transducer"
					devGwTopic := "openchirp/devices/" + update.Id + "/gateway/" + gwid
					c.Unsubscribe(devGwTopic + "/stats")
					if supportTransducerGateway {
						c.Unsubscribe(devTopic + "/stats")
					}
					c.Unsubscribe(devGwTopic + "/rx")
					if supportTransducerGateway {
						c.Unsubscribe(devTopic + "/rx")
					}
					mqttBridge.RemoveLinksAll(update.Id)
					delete(gwidDevice, deviceGwid[update.Id])
					delete(deviceGwid, update.Id)
				}
			case framework.DeviceUpdateTypeUpd:
				logitem.Info("Removing links for update")
				if gwid, ok := deviceGwid[update.Id]; ok {
					devTopic := "openchirp/devices/" + update.Id + "/transducer"
					devGwTopic := "openchirp/devices/" + update.Id + "/gateway/" + gwid
					c.Unsubscribe(devGwTopic + "/stats")
					if supportTransducerGateway {
						c.Unsubscribe(devTopic + "/stats")
					}
					c.Unsubscribe(devGwTopic + "/rx")
					if supportTransducerGateway {
						c.Unsubscribe(devTopic + "/rx")
					}
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
				devGwTopic := "openchirp/devices/" + update.Id + "/gateway/" + gwid
				lsTopic := "gateway/" + gwid

				reportDeviceStatus := func(e error) {
					if e != nil {
						c.SetDeviceStatus(update.Id, "Error linking as \"%s\": %v", gwid, e)
					} else {
						c.SetDeviceStatus(update.Id, "Linked as gateway ", gwid)
					}
				}

				/* Add return tx stream */
				if supportTransducerGateway {
					logitem.Debugf("Adding link %s --> %s, %s", lsTopic+"/tx", devTopic+"/tx", devGwTopic+"/tx")
					err = mqttBridge.AddLinkRev(update.Id, lsTopic+"/tx", devTopic+"/tx", devGwTopic+"/tx")
					if err != nil {
						logitem.Error("Failed to link: ", err)
						reportDeviceStatus(err)
						mqttBridge.RemoveLinksAll(update.Id)
						continue
					}
				} else {
					logitem.Debugf("Adding link %s --> %s", lsTopic+"/tx", devGwTopic+"/tx")
					err = mqttBridge.AddLinkRev(update.Id, lsTopic+"/tx", devGwTopic+"/tx")
					if err != nil {
						logitem.Error("Failed to link: ", err)
						reportDeviceStatus(err)
						mqttBridge.RemoveLinksAll(update.Id)
						continue
					}
				}

				devid := update.Id

				processRx := func(topic string, payload []byte) {
					loglocal := log.WithFields(logrus.Fields{"devid": devid, "gwid": gwid, "pkt": "rx"})

					// forward first
					err := lsMQTT.Publish(lsTopic+"/rx", payload)
					if err != nil {
						loglocal.Warnf("Failed forward rx to %s", lsTopic+"/rx")
					}

					// then parse
					if supportRXPacketStats {
						var rx models.RXPacket
						err = json.Unmarshal(payload, &rx)
						if err != nil {
							loglocal.Warnf("Failed to Unmarshal rx JSON")
							return
						}
						loglocal.Debug("Received rx: ", rx)

						err = c.Publish(devTopic+"/"+topicCRCStatus, fmt.Sprint(rx.RXInfo.CRCStatus))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicCRCStatus, devid)
						}
						err = c.Publish(devTopic+"/"+topicFrequency, fmt.Sprint(rx.RXInfo.Frequency))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicLat, devid)
						}
						err = c.Publish(devTopic+"/"+topicRSSI, fmt.Sprint(rx.RXInfo.RSSI))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicRSSI, devid)
						}
						err = c.Publish(devTopic+"/"+topicLoRaSNR, fmt.Sprint(rx.RXInfo.LoRaSNR))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicLoRaSNR, devid)
						}
						err = c.Publish(devTopic+"/"+topicSpreadingFactor, fmt.Sprint(rx.RXInfo.DataRate.SpreadFactor))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicSpreadingFactor, devid)
						}
						err = c.Publish(devTopic+"/"+topicBandwidth, fmt.Sprint(rx.RXInfo.DataRate.Bandwidth))
						if err != nil {
							loglocal.Errorf("Failed to publish %s for deviceid %s", topicBandwidth, devid)
						}

						if rx.PHYPayload.MHDR.MType == lorawan.UnconfirmedDataUp ||
							rx.PHYPayload.MHDR.MType == lorawan.ConfirmedDataUp {
							devAddrBuf, _ := rx.PHYPayload.MACPayload.(*lorawan.MACPayload).FHDR.DevAddr.MarshalBinary()
							// devAddrBuf[3] = devAddrBuf[3] & 0x01
							devAddr := binary.LittleEndian.Uint32(devAddrBuf)

							nwkID := rx.PHYPayload.MACPayload.(*lorawan.MACPayload).FHDR.DevAddr.NwkID()

							err = c.Publish(devTopic+"/"+topicNetworkID, fmt.Sprint(uint(nwkID)))
							if err != nil {
								loglocal.Errorf("Failed to publish %s for deviceid %s", topicNetworkID, devid)
							}

							err = c.Publish(devTopic+"/"+topicDevAddr, fmt.Sprint(devAddr))
							if err != nil {
								loglocal.Errorf("Failed to publish %s for deviceid %s", topicDevAddr, devid)
							}
						}

					}
				}

				processStats := func(topic string, payload []byte) {
					var stats StatsPacket

					loglocal := log.WithFields(logrus.Fields{"devid": devid, "gwid": gwid, "pkt": "stat"})

					// forward first
					err := lsMQTT.Publish(lsTopic+"/stats", payload)
					if err != nil {
						loglocal.Warnf("Failed forward stats to %s", lsTopic+"/stats")
					}

					// then parse
					err = json.Unmarshal(payload, &stats)
					if err != nil {
						loglocal.Warnf("Failed to Unmarshal stats JSON")
						return
					}
					loglocal.Debug("Received stats: ", stats)

					err = c.Publish(devTopic+"/"+topicLat, fmt.Sprint(stats.Latitude))
					if err != nil {
						loglocal.Errorf("Failed to publish %s for deviceid %s", topicLat, devid)
					}
					err = c.Publish(devTopic+"/"+topicLon, fmt.Sprint(stats.Longitude))
					if err != nil {
						loglocal.Errorf("Failed to publish %s for deviceid %s", topicLon, devid)
					}
					err = c.Publish(devTopic+"/"+topicAlt, fmt.Sprint(stats.Altitude))
					if err != nil {
						loglocal.Errorf("Failed to publish %s for deviceid %s", topicAlt, devid)
					}
					err = c.Publish(devTopic+"/"+topicPktRecv, fmt.Sprint(stats.RxReceived))
					if err != nil {
						loglocal.Errorf("Failed to publish %s for deviceid %s", topicPktRecv, devid)
					}
					err = c.Publish(devTopic+"/"+topicPktRecvOk, fmt.Sprint(stats.RxReceivedOk))
					if err != nil {
						loglocal.Errorf("Failed to publish %s for deviceid %s", topicPktRecvOk, devid)
					}
				}

				/* Add rx streams */
				if supportTransducerGateway {
					logitem.Debugf("Adding processor for %s", devTopic+"/rx")
					err = c.Subscribe(devTopic+"/rx", processRx)
					if err != nil {
						logitem.Error("Failed to link to device rx topic: ", err)
						reportDeviceStatus(err)
						mqttBridge.RemoveLinksAll(update.Id)
						continue
					}
				}
				logitem.Debugf("Adding processor for %s", devGwTopic+"/rx")
				err = c.Subscribe(devGwTopic+"/rx", processRx)
				if err != nil {
					logitem.Error("Failed to link to device rx topic: ", err)
					reportDeviceStatus(err)
					mqttBridge.RemoveLinksAll(update.Id)
					continue
				}

				/* Add stats streams */
				if supportTransducerGateway {
					logitem.Debugf("Adding processor for %s", devTopic+"/stats")
					err = c.Subscribe(devTopic+"/stats", processStats)
					if err != nil {
						logitem.Error("Failed to link to device status topic: ", err)
						reportDeviceStatus(err)
						mqttBridge.RemoveLinksAll(update.Id)
						continue
					}
				}
				logitem.Debugf("Adding processor for %s", devGwTopic+"/stats")
				err = c.Subscribe(devGwTopic+"/stats", processStats)
				if err != nil {
					logitem.Error("Failed to link to device status topic: ", err)
					reportDeviceStatus(err)
					mqttBridge.RemoveLinksAll(update.Id)
					continue
				}

				reportDeviceStatus(nil)
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
