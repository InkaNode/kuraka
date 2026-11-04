import '../i18n/monitoring.js';
import { h, mount } from '../core/h.js';
import { api } from '../core/api.js';
import { t, formatDate } from '../core/i18n.js';
import { button, pageHeader, card, table, badge, field, select, textInput, submitForm, confirmDialog, errorBox } from '../core/ui.js';

function sparkline(points) {
  const ns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(ns, 'svg');
  svg.setAttribute('viewBox', '0 0 240 48');
  svg.setAttribute('class', 'metric-sparkline');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', t('dashboard.history'));
  if (!points.length) return svg;
  const values = points.map(point => point[1]);
  const low = Math.min(...values), high = Math.max(...values);
  const first = points[0][0], span = points.at(-1)[0] - first;
  const line = document.createElementNS(ns, 'polyline');
  line.setAttribute('points', points.map(([at, value]) => `${span ? 2 + (at - first) / span * 236 : 120},${high === low ? 24 : 46 - (value - low) / (high - low) * 44}`).join(' '));
  line.setAttribute('fill', 'none'); line.setAttribute('stroke', 'currentColor'); line.setAttribute('stroke-width', '2');
  svg.append(line);
  return svg;
}

export async function dashboardView() {
  const [metrics, initial] = await Promise.all([api('GET', '/metrics'), api('GET', '/alerts')]);
  let rules = initial;
  const names = Object.keys(metrics.series);
  const list = h('div');
  const errors = h('div');
  let deleting = false;
  async function remove(rule) {
    if (deleting) return;
    deleting = true;
    try {
      if (!await confirmDialog({ title: t('alerts.delete'), message: t('alerts.deleteConfirm', { metric: rule.metric }), danger: true })) return;
      mount(errors);
      await api('DELETE', `/alerts/${encodeURIComponent(rule.id)}`);
      rules = rules.filter(entry => entry.id !== rule.id); renderRules();
    } catch (err) { mount(errors, errorBox(err)); errors.firstElementChild.focus(); }
    finally { deleting = false; }
  }
  function renderRules() {
    mount(list, table({ rows: rules, columns: [
      { label: t('alerts.metric'), className: 'mono', render: rule => rule.metric },
      { label: t('alerts.threshold'), render: rule => rule.threshold },
      { label: t('alerts.state'), render: rule => badge(t(rule.firing_since != null ? 'alerts.firing' : 'alerts.resolved'), rule.firing_since != null ? 'danger' : 'ok') },
      { label: t('alerts.since'), render: rule => rule.firing_since != null ? formatDate(rule.firing_since) : t('queue.none') },
      { label: t('monitoring.actions'), render: rule => button({ label: t('alerts.delete'), variant: 'danger', onClick: () => remove(rule) }) },
    ] }));
  }
  const metric = select({ options: names.map(name => ({ value: name, label: name })), required: true });
  const threshold = textInput({ type: 'number', step: 'any', required: true });
  const form = h('form', { class: 'stack gap-2' },
    field({ label: t('alerts.metric'), input: metric }), field({ label: t('alerts.threshold'), input: threshold }),
    h('p', { class: 'help' }, t('alerts.help')),
    button({ label: t('alerts.add'), icon: 'plus', variant: 'primary', type: 'submit' }));
  submitForm(form, async () => {
    const rule = await api('POST', '/alerts', { metric: metric.value, threshold: Number(threshold.value) });
    rules.push(rule); threshold.value = ''; renderRules();
  });
  renderRules();
  return h('div', { class: 'stack gap-2' }, pageHeader(t('nav.dashboard')),
    !metrics.enabled ? card(h('p', {}, t('dashboard.disabled'))) : null,
    h('div', { class: 'metric-grid' }, names.map(name => card(h('div', { class: 'stack gap-1' },
      h('h2', {}, t(`metric.${name}`)), h('p', { class: 'mono muted' }, name),
      h('strong', { class: 'metric-value' }, metrics.latest[name] ?? t('dashboard.noSamples')), sparkline(metrics.series[name]))))),
    card(h('div', { class: 'stack gap-2' }, h('h2', {}, t('alerts.title')), errors, list, form)));
}
