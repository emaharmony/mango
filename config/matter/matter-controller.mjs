#!/usr/bin/env node
/**
 * Mango Matter Controller — Native Matter device management
 *
 * This script runs as a subprocess of the Mango gateway. It:
 *   - Creates a Matter commissioner fabric
 *   - Discovers devices on the local network
 *   - Handles commissioning via QR code or manual pairing code
 *   - Controls devices (on/off/toggle/brightness/temperature)
 *   - Reports device state changes to Mango via JSON stdout
 *
 * Communication: Mango writes JSON to stdin, this script writes JSON to stdout.
 *
 * Setup: `npm install matter-node.js` in this directory, then `mango matter setup`
 *
 * Required npm packages: @project-chip/matter-node.js
 */

import { CommissioningController, ControllerNode } from "@project-chip/matter-node.js";
import { ServerNode } from "@project-chip/matter-node.js/server";
import { StorageBackendJsonFile } from "@project-chip/matter-node.js/storage";
import { Logger } from "@project-chip/matter-node.js/log";
import { UdpChannelNode } from "@project-chip/matter-node.js/channel";
import { NetworkNode } from "@project-chip/matter-node.js/network";
import { MdnsService } from "@project-chip/matter-node.js/mdns";

import { parseArgs } from "node:util";
import { readFileSync } from "Node:fs";
import { resolve, dirname } from "Node:path";
import { fileURLToPath } from "Node:url";

// ─── JSON message protocol ──────────────────────────────────────────
function sendMessage(type, data) {
  const msg = JSON.stringify({ type, data });
  process.stdout.write(msg + "\n");
}

function sendLog(message) {
  sendMessage("log", { message, timestamp: Date.now() });
}

function sendError(message) {
  sendMessage("error", { message, timestamp: Date.now() });
}

function sendDeviceState(entityId, state, attributes = {}) {
  sendMessage("state", { entity_id: entityId, state, attributes });
}

// ─── CLI args ────────────────────────────────────────────────────────
const { values: args } = parseArgs({
  options: {
    storage: { type: "string", default: "./matter-storage" },
    port: { type: "string", default: "5540" },
    interface: { type: "string" },
  },
  strict: false,
});

const STORAGE_DIR = args.storage;
const MATTER_PORT = parseInt(args.port, 10);
const NETWORK_INTERFACE = args.interface;

let controller = null;
let devices = new Map(); // entityId -> device state

// ─── Controller lifecycle ─────────────────────────────────────────────
async function startController() {
  sendLog("Starting Matter controller...");

  try {
    const storage = new StorageBackendJsonFile(resolve(STORAGE_DIR, "matter-state.json"));

    controller = await CommissioningController.create({
      storage,
      port: MATTER_PORT,
      networkInterface: NETWORK_INTERFACE,
    });

    await controller.start();

    // Load previously commissioned devices
    const commissionedDevices = controller.getCommissionedDevices();
    for (const device of commissionedDevices) {
      await connectDevice(device);
    }

    sendLog(`Matter controller ready — ${commissionedDevices.length} commissioned device(s)`);
    sendMessage("ready", { device_count: commissionedDevices.length });
  } catch (err) {
    sendError(`Failed to start controller: ${err.message}`);
    process.exit(1);
  }
}

async function connectDevice(device) {
  try {
    const endpoints = device.getEndDevices();
    for (const endpoint of endpoints) {
      const entityId = endpointToEntityId(endpoint);
      const state = await readEndpointState(endpoint);
      devices.set(entityId, state);
      sendDeviceState(entityId, state.state, state.attributes);
    }
  } catch (err) {
    sendError(`Failed to connect device: ${err.message}`);
  }
}

// ─── Commissioning ───────────────────────────────────────────────────
async function commissionDevice(code) {
  sendLog(`Commissioning device with code: ${code.substring(0, 10)}...`);

  try {
    const device = await controller.commission({
      pairingCode: code,
    });

    await connectDevice(device);
    sendLog(`Device commissioned successfully`);
    sendMessage("commissioned", { success: true });
  } catch (err) {
    sendError(`Commissioning failed: ${err.message}`);
    sendMessage("commissioned", { success: false, error: err.message });
  }
}

// ─── Device control ──────────────────────────────────────────────────
async function handleCommand(entityId, command, params = {}) {
  const device = findDeviceByEntityId(entityId);
  if (!device) {
    sendError(`Device not found: ${entityId}`);
    return;
  }

  try {
    const endpoint = findEndpoint(device, entityId);

    switch (command) {
      case "on":
        await endpoint.getClusterClient("OnOff").on();
        sendDeviceState(entityId, "on");
        break;
      case "off":
        await endpoint.getClusterClient("OnOff").off();
        sendDeviceState(entityId, "off");
        break;
      case "toggle":
        await endpoint.getClusterClient("OnOff").toggle();
        break;
      case "set_brightness":
        await endpoint.getClusterClient("LevelControl").moveToLevelWithOnOff({
          level: params.brightness || 128,
          transitionTime: params.transition || 0,
        });
        sendDeviceState(entityId, "on", { brightness: params.brightness || 128 });
        break;
      case "set_temperature":
        await endpoint.getClusterClient("Thermostat").setpointLowerHeatAndSetpointUpperHeat({
          lowerHeatSetpoint: params.temperature * 100,
          upperHeatSetpoint: params.temperature * 100,
        });
        sendDeviceState(entityId, "on", { temperature: params.temperature });
        break;
      default:
        sendError(`Unknown command: ${command}`);
    }
  } catch (err) {
    sendError(`Command ${command} failed for ${entityId}: ${err.message}`);
  }
}

// ─── Helpers ──────────────────────────────────────────────────────────
function endpointToEntityId(endpoint) {
  const deviceType = endpoint.getDeviceType();
  const domain = deviceTypeToDomain(deviceType);
  const name = endpoint.id || "unknown";
  return `${domain}.${name}`.replace(/[^a-z0-9_.]/gi, "_").toLowerCase();
}

function deviceTypeToDomain(deviceType) {
  // Map Matter device types to Home Assistant-style domains
  const mapping = {
    0x0100: "light",     // On/Off Light
    0x0101: "light",     // Dimmable Light
    0x010c: "light",     // Color Temperature Light
    0x010a: "switch",    // On/Off Plug
    0x0103: "lock",      // Door Lock
    0x0301: "climate",   // Thermostat
    0x0107: "sensor",    // Temperature Sensor
    0x0402: "fan",       // Fan
  };
  return mapping[deviceType] || "device";
}

async function readEndpointState(endpoint) {
  try {
    const onOff = endpoint.getClusterClient("OnOff");
    if (onOff) {
      const state = await onOff.readAttribute("onOff");
      return { state: state ? "on" : "off", attributes: {} };
    }
  } catch (_) {}
  return { state: "unknown", attributes: {} };
}

function findDeviceByEntityId(entityId) {
  // Walk commissioned devices to find the one with this entity
  for (const device of controller.getCommissionedDevices()) {
    for (const endpoint of device.getEndDevices()) {
      if (endpointToEntityId(endpoint) === entityId) {
        return device;
      }
    }
  }
  return null;
}

function findEndpoint(device, entityId) {
  for (const endpoint of device.getEndDevices()) {
    if (endpointToEntityId(endpoint) === entityId) {
      return endpoint;
    }
  }
  return null;
}

// ─── Stdin command handler ──────────────────────────────────────────
let inputBuffer = "";

process.stdin.on("data", (chunk) => {
  inputBuffer += chunk.toString();
  const lines = inputBuffer.split("\n");
  inputBuffer = lines.pop() || "";

  for (const line of lines) {
    if (!line.trim()) continue;
    try {
      const msg = JSON.parse(line);
      handleMessage(msg);
    } catch (err) {
      sendError(`Invalid JSON from gateway: ${line.substring(0, 100)}`);
    }
  }
});

async function handleMessage(msg) {
  switch (msg.type) {
    case "commission":
      await commissionDevice(msg.code);
      break;
    case "command":
      await handleCommand(msg.entity_id, msg.command, msg.params);
      break;
    case "list":
      const deviceList = Object.fromEntries(devices);
      sendMessage("devices", deviceList);
      break;
    case "shutdown":
      sendLog("Shutting down controller...");
      await shutdown();
      break;
    default:
      sendError(`Unknown message type: ${msg.type}`);
  }
}

async function shutdown() {
  if (controller) {
    await controller.close();
  }
  process.exit(0);
}

// ─── Bootstrap ───────────────────────────────────────────────────────
process.on("SIGINT", shutdown);
process.on("SIGTERM", shutdown);

startController().catch((err) => {
  sendError(`Fatal: ${err.message}`);
  process.exit(1);
});