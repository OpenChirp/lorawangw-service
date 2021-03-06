package main

import (
	"github.com/sirupsen/logrus"
)

type MQTT interface {
	Subscribe(topic string, callback func(topic string, payload []byte)) error
	Unsubscribe(topics ...string) error
	Publish(topic string, payload interface{}) error
}

type BridgeService struct {
	mqtta, mqttb MQTT
	devicelinks  map[string]links
	log          *logrus.Logger
}

// The typical use case is to only append or overwrite a callback
type links struct {
	// mqtta --> mqttb
	fwd []string
	// mqttb --> mqtta
	rev []string
}

func isIn(arr []string, str string) bool {
	for _, s := range arr {
		if s == str {
			return true
		}
	}
	return false
}

func NewBridgeService(mqtta, mqttb MQTT, log *logrus.Logger) *BridgeService {
	b := new(BridgeService)
	b.mqtta = mqtta
	b.mqttb = mqttb
	b.devicelinks = make(map[string]links)
	b.log = log
	// disable logging
	if log == nil {
		b.log = logrus.New()
		b.log.SetLevel(0)
	}
	return b
}

func (b *BridgeService) IsDeviceLinked(deviceid string) bool {
	_, ok := b.devicelinks[deviceid]
	return ok
}

func (b *BridgeService) IsLinkFwd(deviceid, topica string) bool {
	if ls, ok := b.devicelinks[deviceid]; ok {
		for _, l := range ls.fwd {
			if l == topica {
				return true
			}
		}
	}
	return false
}

func (b *BridgeService) IsLinkRev(deviceid, topicb string) bool {
	if ls, ok := b.devicelinks[deviceid]; ok {
		for _, l := range ls.rev {
			if l == topicb {
				return true
			}
		}
	}
	return false
}

func (b *BridgeService) AddLinkFwd(deviceid, topica string, topicb ...string) error {
	ls, ok := b.devicelinks[deviceid]
	if !ok {
		ls = links{make([]string, 0), make([]string, 0)}
	}

	// Mark down our link
	if !isIn(ls.fwd, topica) {
		ls.fwd = append(ls.fwd, topica)
	}

	// Subscribe
	err := b.mqtta.Subscribe(topica, func(topic string, payload []byte) {
		for _, tb := range topicb {
			b.log.Debugf("Received on %s and publishing to %s", topic, tb)
			if err := b.mqttb.Publish(tb, payload); err != nil {
				b.log.Errorf("Failed to publish to %s: %v", tb, err)
			}
		}

	})
	if err != nil {
		return err
	}

	b.devicelinks[deviceid] = ls
	return nil
}

func (b *BridgeService) AddLinkRev(deviceid, topicb string, topica ...string) error {
	ls, ok := b.devicelinks[deviceid]
	if !ok {
		ls = links{make([]string, 0), make([]string, 0)}
	}

	// Mark down our link
	if !isIn(ls.rev, topicb) {
		ls.rev = append(ls.rev, topicb)
	}

	// Subscribe
	err := b.mqttb.Subscribe(topicb, func(topic string, payload []byte) {
		for _, ta := range topica {
			b.log.Debugf("Received on %s and publishing to %v", topic, ta)
			if err := b.mqtta.Publish(ta, payload); err != nil {
				b.log.Errorf("Failed to publish to %s: %v", ta, err)
			}
		}

	})
	if err != nil {
		return err
	}

	// Commit changes
	b.devicelinks[deviceid] = ls

	return nil
}

func (b *BridgeService) RemoveLinksAll(deviceid string) error {
	var err error
	if ls, ok := b.devicelinks[deviceid]; ok {
		if len(ls.fwd) > 0 {
			e := b.mqtta.Unsubscribe(ls.fwd...)
			// save and return only first error
			if e != nil && err == nil {
				err = e
			}
		}
		if len(ls.rev) > 0 {
			e := b.mqttb.Unsubscribe(ls.rev...)
			// save and return only first error
			if e != nil && err == nil {
				err = e
			}
		}
		ls.fwd = nil
		ls.rev = nil
		delete(b.devicelinks, deviceid)
	}
	return err
}
