// <gv-chart data-option='{...}' data-height="280"> renders an ECharts option.
//
// The chart lives in a shadow root, so Datastar's DOM morphing (which matches
// elements by id and rewrites attributes) never touches the rendered SVG: it
// only changes data-option, and attributeChangedCallback re-renders.
//
// Options are JSON, so they cannot carry functions. Strings under a
// `formatter` / `valueFormatter` key that start with "gv:" name a formatter
// from FORMATTERS below ("gv:mgdl", "gv:point:mmol:hhmm", ...). Other strings
// are ordinary ECharts templates ("{b}: {c}%").
//
// Colours for text, axes and tooltips come from the app's CSS custom
// properties (--ink, --ink-2, --line, --surface), which inherit into the
// shadow root, so the charts follow light/dark mode with no server round trip.
(() => {
  const pad = (n) => String(n).padStart(2, '0');
  const num = (v) => (Array.isArray(v) ? v[v.length - 1] : v);
  // Numbers follow the page's language (<html lang>): "5,6" in German.
  const LANG = document.documentElement.lang || 'en';
  const nf = (v, min, max) => new Intl.NumberFormat(LANG, { minimumFractionDigits: min, maximumFractionDigits: max }).format(v);

  // Value formatters, by name. hhmm takes minutes since midnight.
  const FMT = {
    mgdl: (v) => String(Math.round(v)),
    mmol: (v) => nf(v, 1, 1),
    pct: (v) => (Math.abs(v - Math.round(v)) < 0.05 ? nf(Math.round(v), 0, 0) : nf(v, 1, 1)) + '%',
    int: (v) => String(Math.round(v)),
    bpm: (v) => Math.round(v) + ' bpm',
    m: (v) => Math.round(v) + ' m',
    num: (v) => nf(v, 0, 2),
    hhmm: (v) => v >= 1440 ? '24:00' : pad(Math.floor(v / 60) % 24) + ':' + pad(Math.round(v % 60) % 60),
    min: (v) => {
      const m = Math.round(v);
      return m >= 60 ? Math.floor(m / 60) + 'h ' + pad(m % 60) + 'm' : m + ' min';
    },
  };
  const fmt = (name) => FMT[name] || FMT.num;
  // Axis-label and tooltip callbacks get either a bare value or {value}.
  const bare = (p) => (p !== null && typeof p === 'object' && 'value' in p ? p.value : p);

  const FORMATTERS = {
    // "gv:mgdl": single value formatter (axis labels, valueFormatter).
    value: (name) => (p) => fmt(name)(num(bare(p))),
    // "gv:rawaxis:<fmt>": axis tooltip for stacked series whose items are
    // [x, stackedValue, realValue]; shows realValue.
    rawaxis: (name) => (ps) => {
      const rows = Array.isArray(ps) ? ps : [ps];
      const f = fmt(name);
      const head = rows[0] ? FMT.hhmm(rows[0].data[0]) : '';
      const body = rows
        .filter((r) => r.data && r.data[2] != null)
        .map((r) => r.marker + r.seriesName + ': <b>' + f(r.data[2]) + '</b>');
      return head + '<br/>' + body.join('<br/>');
    },
    // "gv:point:<xfmt>:<yfmt>": scatter item, name plus both values.
    point: (xf, yf) => (p) =>
      (p.name ? p.name + '<br/>' : '') + fmt(xf)(p.value[0]) + ' / ' + fmt(yf)(p.value[1]),
    // "gv:cell:<fmt>": heatmap item [x, y, value] whose name is the label.
    cell: (name) => (p) => p.name + '<br/><b>' + fmt(name)(p.value[2]) + '</b>',
  };

  function resolve(spec) {
    const [kind, ...args] = spec.slice(3).split(':');
    if (FORMATTERS[kind]) return FORMATTERS[kind](...args);
    // "gv:mgdl" style: the kind is itself a value format.
    return FORMATTERS.value(kind);
  }

  // Replace "gv:" formatter names with functions, in place.
  function revive(node) {
    if (Array.isArray(node)) return node.forEach(revive);
    if (node === null || typeof node !== 'object') return;
    for (const k of Object.keys(node)) {
      const v = node[k];
      if ((k === 'formatter' || k === 'valueFormatter') && typeof v === 'string' && v.startsWith('gv:')) {
        node[k] = resolve(v);
      } else {
        revive(v);
      }
    }
  }

  function palette(el) {
    const cs = getComputedStyle(el);
    const get = (n, d) => cs.getPropertyValue(n).trim() || d;
    const dark = document.documentElement.dataset.mode
      ? document.documentElement.dataset.mode === 'dark'
      : matchMedia('(prefers-color-scheme: dark)').matches;
    return {
      ink: get('--ink', dark ? '#e5e7eb' : '#111827'),
      ink2: get('--ink-2', dark ? '#9ca3af' : '#4b5563'),
      line: get('--line', dark ? '#374151' : '#e5e7eb'),
      surface: get('--surface', dark ? '#1f2937' : '#ffffff'),
      font: get('--font', 'system-ui, sans-serif'),
    };
  }

  function themeFor(c) {
    const axis = {
      axisLine: { lineStyle: { color: c.line } },
      axisTick: { lineStyle: { color: c.line } },
      axisLabel: { color: c.ink2 },
      splitLine: { lineStyle: { color: c.line, opacity: 0.7 } },
      nameTextStyle: { color: c.ink2 },
    };
    return {
      backgroundColor: 'transparent',
      textStyle: { color: c.ink2, fontFamily: c.font },
      title: { textStyle: { color: c.ink, fontFamily: c.font }, subtextStyle: { color: c.ink2, fontFamily: c.font } },
      legend: { textStyle: { color: c.ink2 }, inactiveColor: c.line },
      categoryAxis: axis,
      valueAxis: axis,
      timeAxis: axis,
      logAxis: axis,
      tooltip: {
        confine: true,
        backgroundColor: c.surface,
        borderColor: c.line,
        textStyle: { color: c.ink, fontFamily: c.font, fontSize: 12 },
        axisPointer: { lineStyle: { color: c.ink2 }, crossStyle: { color: c.ink2 } },
      },
      dataZoom: { textStyle: { color: c.ink2 }, borderColor: c.line, fillerColor: 'rgba(128,128,128,.15)' },
      visualMap: { textStyle: { color: c.ink2 } },
    };
  }

  const reduced = () => matchMedia('(prefers-reduced-motion: reduce)').matches;

  // Current zoom of each dataZoom component, so a data refresh keeps the view
  // the person zoomed to as long as the zoom definition itself is unchanged.
  const zoomState = (chart) =>
    ((chart.getOption() || {}).dataZoom || []).map((z) => ({ start: z.start, end: z.end }));

  class GvChart extends HTMLElement {
    static observedAttributes = ['data-option', 'data-height'];

    connectedCallback() {
      if (!this.shadowRoot) {
        const root = this.attachShadow({ mode: 'open' });
        root.innerHTML = '<style>:host{display:block;width:100%;position:relative}.c{width:100%;height:100%}</style><div class="c"></div>';
        this._box = root.querySelector('.c');
      }
      this.style.display = 'block';
      this._applyHeight();
      this._ro = new ResizeObserver(() => this._chart && this._chart.resize());
      this._ro.observe(this);
      this._mo = new MutationObserver(() => this._render(true));
      this._mo.observe(document.documentElement, { attributes: true, attributeFilter: ['data-mode'] });
      this._mq = matchMedia('(prefers-color-scheme: dark)');
      this._mqFn = () => this._render(true);
      this._mq.addEventListener('change', this._mqFn);
      this._render(true);
    }

    disconnectedCallback() {
      if (this._ro) this._ro.disconnect();
      if (this._mo) this._mo.disconnect();
      if (this._mq) this._mq.removeEventListener('change', this._mqFn);
      if (this._chart) this._chart.dispose();
      this._chart = null;
    }

    attributeChangedCallback(name, oldVal, newVal) {
      if (!this.isConnected || oldVal === newVal) return;
      if (name === 'data-height') {
        this._applyHeight();
        if (this._chart) this._chart.resize();
      } else {
        this._render(false);
      }
    }

    _applyHeight() {
      const h = parseInt(this.getAttribute('data-height'), 10);
      if (h > 0) this.style.height = h + 'px';
    }

    // fresh: rebuild the ECharts instance (theme change); otherwise update it.
    _render(fresh) {
      let opt;
      try {
        opt = JSON.parse(this.getAttribute('data-option') || '{}');
      } catch (e) {
        console.error('gv-chart: bad data-option', e);
        return;
      }
      if (!window.echarts || !this._box) return;

      const prev = this._chart && this._raw;
      const keepZoom = prev && JSON.stringify(prev.dataZoom) === JSON.stringify(opt.dataZoom);
      const zoom = keepZoom ? zoomState(this._chart) : null;

      if (fresh || !this._chart) {
        if (this._chart) this._chart.dispose();
        echarts.registerTheme('gv', themeFor(palette(this)));
        this._chart = echarts.init(this._box, 'gv', { renderer: 'svg' });
      }
      this._raw = JSON.parse(JSON.stringify(opt));
      if (zoom && Array.isArray(opt.dataZoom)) {
        opt.dataZoom.forEach((z, i) => {
          if (zoom[i] && zoom[i].start != null) {
            delete z.startValue;
            delete z.endValue;
            z.start = zoom[i].start;
            z.end = zoom[i].end;
          }
        });
      }
      revive(opt);
      opt.animation = opt.animation !== false && !reduced();
      opt.aria = Object.assign({ enabled: true }, opt.aria);
      this._chart.setOption(opt, { notMerge: true });
    }
  }

  if (!customElements.get('gv-chart')) customElements.define('gv-chart', GvChart);
})();
