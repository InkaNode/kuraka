import '../i18n/docs.js';
import '../i18n/domains.js';
import '../i18n/admin.js';
import '../i18n/integrations.js';
import '../i18n/monitoring.js';
import { h } from '../core/h.js';
import { t } from '../core/i18n.js';
import { card, pageHeader } from '../core/ui.js';

export async function docsView(ctx) {
  const p = (key, vars) => h('p', {}, t(`docs.${key}`, vars));
  const steps = (...items) => h('ol', { class: 'stack gap-1' }, items.map(text => h('li', {}, text)));
  const tip = (key, vars) => h('p', { class: 'help' }, t(`docs.${key}`, vars));
  const sections = [
    ['start',
      p('start.intro'),
      p('start.roles', { superadmin: t('admin.role.superadmin'), tenantAdmin: t('admin.role.tenant_admin'), tenants: t('nav.tenants') }),
      steps(t('docs.start.domain'), t('docs.start.dns'), t('docs.start.mailboxes'), t('docs.start.test'))],
    ['domains',
      tip('domains.role', { role: t('admin.role.superadmin') }),
      steps(
        t('docs.domains.open', { domains: t('nav.domains'), add: t('domains.add'), tenant: t('domains.col.tenant') }),
        t('docs.domains.tenant', { add: t('domains.add.tenant.new'), name: t('domains.add.tenant.name') }),
        t('docs.domains.create', { name: t('domains.add.name'), aliases: t('domains.add.aliases'), add: t('domains.add') })),
      p('domains.import'),
      p('domains.next', { checklist: t('setup.title') })],
    ['dns',
      p('dns.intro', { tab: t('domain.tab.dns') }),
      h('h3', {}, t('docs.dns.automatic')),
      steps(
        t('docs.dns.connect', { integrations: t('nav.integrations'), connect: t('integrations.connectDNS'), token: t('integrations.token') }),
        t('docs.dns.select', { tab: t('domain.tab.dns'), provider: t('dns.provider'), publish: t('dns.publish') }),
        t('docs.dns.plan', { replace: t('dns.plan.replace'), apply: t('dns.plan.apply') })),
      h('p', { class: 'help' }, t('dns.plan.replace.warning')),
      h('h3', {}, t('docs.dns.manual')),
      steps(
        t('docs.dns.copy', { tab: t('domain.tab.dns'), copy: t('copy'), zone: t('dns.copyZone') }),
        t('docs.dns.verify', { verify: t('dns.check') })),
      tip('dns.checkSource'),
      h('ul', { class: 'stack gap-1' }, ['mx', 'spf', 'dkim', 'dmarc'].map(key => h('li', {}, t(`docs.dns.${key}`)))),
      tip('dns.ptr'), tip('dns.wait')],
    ['mailboxes',
      steps(
        t('docs.mailboxes.create', { tab: t('domain.tab.mailboxes'), add: t('mailbox.new'), name: t('mailbox.local'), displayName: t('mailbox.col.name'), create: t('create') }),
        t('docs.mailboxes.password', { generate: t('mailbox.password.auto') }),
        t('docs.mailboxes.alias', { aliases: t('mailbox.col.aliases'), add: t('mailbox.alias.add') })),
      p('mailboxes.connect'),
      tip('mailboxes.shared', { shared: t('mailbox.shared') })],
    ['aliases',
      p('aliases.intro'),
      steps(
        t('docs.aliases.add', { tab: t('domain.tab.aliases'), input: t('aliasdomains.input'), add: t('aliasdomains.add') }),
        t('docs.aliases.dns', { tab: t('domain.tab.dns') }))],
    ['migrations',
      steps(
        t('docs.migrations.open', { tab: t('domain.tab.migrations'), add: t('migration.new'), target: t('migration.target') }),
        t('docs.migrations.source', { host: t('migration.host'), port: t('migration.port'), user: t('migration.user'), password: t('migration.password') }),
        t('docs.migrations.tls', { security: t('migration.tls'), tls: t('migration.tls.tls'), starttls: t('migration.tls.starttls'), none: t('migration.tls.none') }),
        t('docs.migrations.run', { submit: t('migration.submit'), tab: t('domain.tab.migrations') })),
      h('p', { class: 'help' }, t('migration.tls.none.warning')),
      h('p', {}, t('migration.intro')),
      h('p', { class: 'help' }, t('migration.secret.note'))],
    ['integrations',
      p('integrations.dns', { integrations: t('nav.integrations'), connect: t('integrations.connectDNS'), scope: t('integrations.scope') }),
      steps(
        t('docs.integrations.helpdesk', { connect: t('integrations.connectHelpdesk'), tenant: t('integrations.tenant'), url: t('integrations.url'), key: t('integrations.apiKey'), secret: t('integrations.apiSecret') }),
        t('docs.integrations.mailbox', { tab: t('domain.tab.mailboxes'), support: t('helpdesk.action'), helpdesk: t('helpdesk.select'), connect: t('helpdesk.connect.submit') })),
      h('p', { class: 'help' }, t('helpdesk.intro')),
      tip('integrations.bundle', { role: t('admin.role.superadmin'), bundle: t('integrations.bundle') })],
    ['alerts',
      tip('alerts.role', { role: t('admin.role.superadmin') }),
      steps(
        t('docs.alerts.open', { dashboard: t('nav.dashboard'), alerts: t('alerts.title') }),
        t('docs.alerts.rule', { metric: t('alerts.metric'), threshold: t('alerts.threshold'), add: t('alerts.add') })),
      h('p', { class: 'help' }, t('alerts.help')),
      p('alerts.webhook'),
      p('alerts.email')],
    ['security',
      steps(
        t('docs.security.password', { account: t('nav.account'), change: t('account.passwordTitle'), current: t('account.current'), next: t('account.new'), confirm: t('account.confirm') }),
        t('docs.security.enable', { totp: t('account.totp'), enable: t('account.enable') }),
        t('docs.security.confirm', { code: t('account.code'), confirm: t('account.verify') })),
      tip('security.disable', { disable: t('account.disable') })],
    ['troubleshooting',
      p('troubleshooting.spam', { verify: t('dns.check') }),
      p('troubleshooting.conflicts', { publish: t('dns.publish'), replace: t('dns.plan.replace') }),
      h('blockquote', {}, h('p', {}, t('error.upstream_forbidden'))),
      p('troubleshooting.forbidden'),
      p('troubleshooting.wait', { verify: t('dns.check') })],
  ];
  const cards = sections.map(([id, ...content]) => {
    const section = card(h('h2', { id: `${id}-title` }, t(`docs.${id}.title`)), ...content);
    section.id = id;
    section.classList.add('stack', 'gap-2');
    section.setAttribute('tabindex', '-1');
    section.setAttribute('aria-labelledby', `${id}-title`);
    return section;
  });
  const root = h('div', { class: 'docs-page stack gap-2' }, pageHeader(t('nav.docs')),
    h('p', { class: 'muted' }, t('docs.intro')),
    h('nav', { class: 'stack gap-2', 'aria-labelledby': 'docs-toc' },
      h('h2', { id: 'docs-toc' }, t('docs.toc')),
      h('ul', { class: 'stack gap-1' }, sections.map(([id]) => h('li', {},
        h('a', { href: `#/docs?s=${id}` }, t(`docs.${id}.title`)))))),
    cards);
  const target = cards.find(section => section.id === ctx.query.get('s'));
  if (target) requestAnimationFrame(() => {
    // The router mounts the view and focuses its page heading before this frame.
    if (!target.isConnected) return;
    target.focus({ preventScroll: true });
    target.scrollIntoView({ block: 'start' });
  });
  return root;
}
