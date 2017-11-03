# lorawangw-service
LoRaWAN Gateway Service for OpenChirp

This service's role is to support LoRaWAN gateways as Devices in the OpenChirp
framework. The service achieves this by binding to the three gateway control
subtopics used by the [lora-gateway-bridge](https://github.com/brocaar/lora-gateway-bridge) within the linked to OpenChirp
Device's MQTT root. For an OpenChirp Device with id 59af7391f230cf7055614d0e,
the MQTT gateway control topics would be the following:
* openchirp/devices/59af7391f230cf7055614d0e/rx
* openchirp/devices/59af7391f230cf7055614d0e/tx
* openchirp/devices/59af7391f230cf7055614d0e/stats

In order for this to be possible, [lora-gateway-bridge](https://github.com/brocaar/lora-gateway-bridge), which runs on the gateway,
must have it's default MQTT path changed to the Device's MQTT root as shown
above.

Within the service, the three control subtopics are mapped/bridged into a
globally specified target MQTT broker's gateway/ topic tree. The idea is to map
many Device gateways into one combined loraserver MQTT topic tree that is
compatible with [loraserver](https://github.com/brocaar/loraserver).
Additionally, this combined MQTT topic tree can be on a foreign MQTT broker.

## Usage

At the minimum, `lorawangwservice` needs the OpenChirp framework URI(`-framework`), broker URI(`-broker`), service id (`-id`), and and service token (`-token`).

The target loraserver MQTT broker can have it's MQTT parameters set in a few different manors.
* Setting them on the commandline will bypass all other methods. Use `-lsbroker`, `-lsqos`, `-lsuser`, `-lspass` to set them on the commandline.
* If the broker settings are not present on the commandline, the service will try to acquire them from the OpenChirp service properties set through the website. It will look for `MQTTBroker`, `MQTTQoS`, `MQTTUser`, and `MQTTPas` properties.
* If both the previous methods did not resolve the broker settings, the service will simply use the framework's MQTT parameters.

## Developement
This project uses `govendor` for Go vendoring.
