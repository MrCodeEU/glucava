# Vendored: Apache ECharts

| | |
|---|---|
| File | `echarts.min.js` (custom tree-shaken build, SVG renderer only) |
| Version | echarts 6.1.0 (zrender 6.1.0) |
| Source | https://registry.npmjs.org/echarts/-/echarts-6.1.0.tgz (sha1 `ae0f68590f5ebbd728d900907c27acde7c5456d1`) |
| Size | 719,531 B raw, 246,060 B gzip (full `dist/echarts.min.js` is ~1.1 MB / 369 KB gzip) |
| sha256 | `25bcf6ce8954617a43cb497d55028c0864ca44febd9cfad48f8cfd2386af4f66` |
| Licence | Apache-2.0, text and NOTICE in `echarts.LICENSE.txt` (esbuild strips the in-file banners) |

Exposes the global `echarts`. Included: line, bar, pie, heatmap, scatter charts;
grid, tooltip, legend, dataZoom, visualMap, markArea, markLine, title, aria
components; SVG renderer. Add anything else to `entry.js` and rebuild.

## Rebuild

```sh
mkdir build && cd build
npm init -y && npm i echarts@6.1.0 esbuild@0.25 --save-exact
cat > entry.js <<'JS'
import * as echarts from 'echarts/core';
import { LineChart, BarChart, PieChart, HeatmapChart, ScatterChart } from 'echarts/charts';
import { GridComponent, TooltipComponent, LegendComponent, DataZoomComponent, VisualMapComponent, MarkAreaComponent, MarkLineComponent, TitleComponent, AriaComponent } from 'echarts/components';
import { SVGRenderer } from 'echarts/renderers';
echarts.use([LineChart, BarChart, PieChart, HeatmapChart, ScatterChart, GridComponent, TooltipComponent, LegendComponent, DataZoomComponent, VisualMapComponent, MarkAreaComponent, MarkLineComponent, TitleComponent, AriaComponent, SVGRenderer]);
globalThis.echarts = echarts;
JS
npx esbuild entry.js --bundle --minify --format=iife --legal-comments=none --outfile=echarts.min.js
```

The output is not byte-reproducible across esbuild versions; the sha256 above
is of the committed file.
