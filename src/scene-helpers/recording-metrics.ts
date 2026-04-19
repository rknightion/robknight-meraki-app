/**
 * Canonical metric names emitted by the bundled Grafana-managed recording
 * rules (see `pkg/plugin/recordings/`). The constants here are the single
 * source of truth shared between:
 *   - the Go template YAMLs that instruct Grafana to emit these series, and
 *   - the scene-panel trend queries that read them back from the operator's
 *     Prometheus sink.
 *
 * Drift between the two is a class of bug the plan's §1.14 invariants try
 * to prevent; keeping this file next to `trendQuery(...)` — which imports
 * from here — makes the coupling explicit.
 *
 * All names must match `^meraki_[a-z][a-z0-9_]*$` (enforced on the Go side
 * at template-load time). DO NOT pluralise inconsistently — gauges use
 * singular `_count` suffixes; rates use `_per_second`; percentages use
 * `_pct`.
 */
export const MERAKI_RECORDING_METRICS = {
  /** availability/device-status-overview — counts by status label. */
  deviceStatusCount: 'meraki_device_status_count',
  /** wan/appliance-uplinks-overview — active uplink counts by kind. */
  applianceUplinksActiveCount: 'meraki_appliance_uplinks_active_count',
  /** wan/appliance-uplink-status — per-uplink binary up/down gauge. */
  applianceUplinkUp: 'meraki_appliance_uplink_up',
  /** wan/appliance-vpn-summary — percent of VPN tunnels up per network/peer. */
  applianceVpnTunnelsUpPct: 'meraki_appliance_vpn_tunnels_up_pct',
  /** wan/device-uplinks-loss-latency (cat B) — per-uplink loss percent. */
  wanUplinkLossPct: 'meraki_wan_uplink_loss_pct',
  /** switches/ports-overview — switch port counts by state/speed/media. */
  switchPortsCount: 'meraki_switch_ports_count',
  /** wireless/ap-client-count — per-AP connected client counts. */
  apClientCount: 'meraki_ap_client_count',
  /** wireless/channel-util-history (cat B) — per-AP band channel util %. */
  wirelessChannelUtilPct: 'meraki_wireless_channel_util_pct',
  /** wireless/usage-history (cat B) — per-AP band bytes sent+received. */
  wirelessUsageBytes: 'meraki_wireless_usage_bytes',
  /** alerts/alerts-overview-by-type — counts per (type, severity). */
  alertsByTypeCount: 'meraki_alerts_by_type_count',
  /** alerts/alerts-overview-by-network — counts per (network, severity). */
  alertsByNetworkCount: 'meraki_alerts_by_network_count',
  /** wireless/packet-loss-by-network — per-network wireless packet-loss %. */
  wirelessPacketLossPct: 'meraki_wireless_packet_loss_pct',
  /** cellular/mg-uplink-signal — per-MG cellular RSRP dBm (+ rsrq / sinr). */
  mgRsrpDbm: 'meraki_mg_rsrp_dbm',
  /** alerts/alerts-history-by-severity (cat B) — historical alert counts. */
  alertsHistoryCount: 'meraki_alerts_history_count',
} as const;

export type MerakiRecordingMetric =
  (typeof MERAKI_RECORDING_METRICS)[keyof typeof MERAKI_RECORDING_METRICS];
