# Mattermost Metrics Plugin

The Mattermost Metrics Plugin is a versatile utility designed to collect and store various data points from the Mattermost application in [OpenMetrics](https://openmetrics.io/) format, similar to the functionality provided by [Prometheus](https://prometheus.io/). The primary purpose of this plugin is to facilitate troubleshooting by gathering metrics at regular intervals and allowing their inclusion in a dump file.

## Operational Modes

In a High Availability (HA) environment, the Mattermost Metrics Plugin operates in two distinct modes: scraper mode and listener mode.

- **Scraper Mode:** In this mode, the plugin scrapes metrics from nodes listed in the `ClusterDiscovery` table. It also includes tsdb synchronization to a remote file store. This mode is essential for collecting metrics data.

- **Listener Mode:** In listener mode, the plugin serves download requests by copying tsdb blocks from the remote file store and preparing the dump. This mode is focused on making the collected metrics available for analysis and troubleshooting.

For the single node deployment, these two modes are combined.

## Getting Started

To install the plugin, follow these steps:

1. Download the latest version from the [release page](https://github.com/mattermost/mattermost-plugin-metrics/releases).
2. Upload the downloaded file through **System Console > Plugins > Plugin Management**, or manually place it in the Mattermost server's plugin directory.
3. Enable the plugin.

## Contribution Guidelines

If you wish to contribute to the Mattermost Metrics Plugin, ensure you have the following versions installed:

- Go: version >= **1.20**
- NodeJS: version **16.x**
- NPM: version **7.x**

Feel free to join the [Developers: Performance](https://community.mattermost.com/core/channels/developers-performance) channel to engage in discussions related to the project.

## License

See [LICENSE](LICENSE) for licensing information. Your contributions to this open-source project are welcome!
