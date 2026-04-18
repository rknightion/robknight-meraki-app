# Screenshots

These PNGs are referenced from `src/plugin.json` `info.screenshots` and are
shipped in the plugin bundle so the Grafana plugin catalogue can display
them. They must be captured from a live Grafana instance running this
plugin against a real (or lab) Meraki org — the CI build cannot produce
them.

## How to refresh

1. Bring up the dev stack (Grafana + the plugin): `npm run server`.
2. Configure a valid Meraki Dashboard API key on the Configuration page.
3. For each target page below, resize the browser so the viewport is ~1200
   × 800 CSS pixels, screenshot the full page, and save to this directory
   under the filename listed below.

## Targets

| File                              | Page                   | What to capture                                                                             |
| --------------------------------- | ---------------------- | ------------------------------------------------------------------------------------------- |
| `01-configuration.png`            | `/a/<id>/configuration` | The config form with a saved API key, Region picker visible, Test Connection result green. |
| `02-home.png`                     | `/a/<id>/home`          | HomeIntro + Organizations stat + device-status donut + org inventory table (populated).     |
| `03-sensors-timeseries.png`       | `/a/<id>/sensors`       | KPI row + at least two timeseries cards with real data + inventory table.                   |

## Guidance

- Target ~1200 × 800 px per Grafana publish docs.
- PNG, 8-bit, no alpha channel. Compress with `pngquant --quality=80-90` or
  similar if the raw file exceeds ~500 KB — the plugin bundle has a soft
  size budget.
- No API keys, customer names, or PII in the frame. Redact as needed
  before committing.
- Dark theme is fine; make sure the banner and actions are in the viewport
  so the screenshot tells the whole story at a glance.
