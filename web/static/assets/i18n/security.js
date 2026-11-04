import { registerMessages } from '../core/i18n.js';

registerMessages({
  es: {
    'nav.security': 'Seguridad',
    'security.intro': 'Stalwart bloquea solo a las direcciones que parecen un ataque (muchos intentos fallidos o escaneo de puertos). Aquí puedes ver y quitar esos bloqueos.',
    'security.internalWarning': 'Una dirección interna del servidor está bloqueada. Suele ser el proxy (Caddy): mientras siga bloqueada, el correo web puede dar error 502.',
    'security.address': 'Dirección', 'security.reason': 'Motivo', 'security.since': 'Desde', 'security.until': 'Hasta', 'security.type': 'Tipo',
    'security.reason.portScanning': 'Escaneo de puertos', 'security.forever': 'Para siempre',
    'security.internal': 'Interna', 'security.external': 'Externa',
    'security.fix': 'Arreglar', 'security.unblock': 'Desbloquear',
    'security.fixConfirm': 'Se quitará el bloqueo a {address} y su red interna ({trust}) quedará como de confianza, para que no se vuelva a bloquear. No afecta tus correos.',
    'security.unblockConfirm': 'Se quitará el bloqueo a {address}. Si vuelve a atacar, Stalwart la bloqueará otra vez.',
    'security.empty': 'No hay direcciones bloqueadas.',
  },
  en: {
    'nav.security': 'Security',
    'security.intro': 'Stalwart blocks addresses that look like attacks (many failed logins or port scans). Here you can see and remove those blocks.',
    'security.internalWarning': 'An internal server address is blocked. It is usually the proxy (Caddy): while it stays blocked, webmail can show error 502.',
    'security.address': 'Address', 'security.reason': 'Reason', 'security.since': 'Since', 'security.until': 'Until', 'security.type': 'Type',
    'security.reason.portScanning': 'Port scan', 'security.forever': 'Forever',
    'security.internal': 'Internal', 'security.external': 'External',
    'security.fix': 'Fix', 'security.unblock': 'Unblock',
    'security.fixConfirm': 'This removes the block on {address} and marks its internal network ({trust}) as trusted so it is not blocked again. Your email is not affected.',
    'security.unblockConfirm': 'This removes the block on {address}. If it attacks again, Stalwart will block it again.',
    'security.empty': 'No blocked addresses.',
  },
});
