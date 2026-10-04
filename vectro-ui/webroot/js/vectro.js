// Vectro pages: overview/diagnostics, voice & Home Assistant config, and
// the time zone list for preferences.

function esc(s) {
    return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// rows: [[label, value, cls?], ...]; values are escaped.
function renderKV(id, rows) {
    document.getElementById(id).innerHTML = rows
        .filter(r => r[1] !== undefined && r[1] !== null && r[1] !== '')
        .map(([k, v, cls]) => `<dt>${esc(k)}</dt><dd class="${cls || ''}">${esc(v)}</dd>`)
        .join('') || '<dt>—</dt><dd>no data</dd>';
}

function setPill(id, text, cls) {
    const el = document.getElementById(id);
    el.textContent = text;
    el.className = 'pill ' + (cls || '');
}

function duration(s) {
    if (s == null) return '';
    const d = Math.floor(s / 86400), h = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
    return (d ? d + 'd ' : '') + (h ? h + 'h ' : '') + m + 'm';
}

function batteryClass(level) {
    return { full: 'ok', nominal: 'ok', low: 'warn', unknown: '' }[level] ?? '';
}

async function loadDiagnostics() {
    let d;
    try {
        const res = await fetch('/api/mods/Diagnostics/report');
        d = await res.json();
    } catch (e) {
        setPill('pillName', 'offline', 'bad');
        return;
    }
    const sys = d.system || {}, net = d.network || {}, rob = d.robot || {}, links = d.vectro || {};

    setPill('pillName', (sys.hostname || 'Vector').replace(/^Vector-/, 'Vectro '), 'ok');
    if (rob.battery_volts) {
        const charging = rob.charging ? ' ⚡' : (rob.on_charger ? ' (docked)' : '');
        setPill('pillBattery', `${rob.battery_volts} V${charging}`, batteryClass(rob.battery_level));
    }
    const ha = links.home_assistant;
    if (ha) setPill('pillHA', ha.ok ? 'HA connected' : 'HA unreachable', ha.ok ? 'ok' : 'bad');

    const fault = document.getElementById('diagFault');
    fault.hidden = !d.fault;
    fault.textContent = d.fault ? `Fault code ${d.fault} is pending on the robot.` : '';

    const flags = rob.status || [];
    const alarming = ['picked up', 'falling', 'cliff detected', 'being held'];
    renderKV('diagRobot', [
        ['Battery', rob.battery_volts ? `${rob.battery_volts} V (${rob.battery_level})` : rob.battery_error, batteryClass(rob.battery_level) + '-text'],
        ['Charging', rob.charging === undefined ? '' : (rob.charging ? 'yes' : 'no')],
        ['On charger', rob.on_charger === undefined ? '' : (rob.on_charger ? 'yes' : 'no')],
        ['State', flags.length ? flags.join(', ') : (rob.error || rob.state_error || 'idle'),
            flags.some(f => alarming.includes(f)) ? 'warn-text' : ''],
    ]);
    renderKV('diagSensors', [
        ['Body pitch', rob.pitch_deg !== undefined ? `${rob.pitch_deg}°` : '', Math.abs(rob.pitch_deg) > 15 ? 'warn-text' : ''],
        ['Head angle', rob.head_angle_deg !== undefined ? `${rob.head_angle_deg}°` : ''],
        ['Lift height', rob.lift_height_mm !== undefined ? `${rob.lift_height_mm} mm` : ''],
        ['Accel (mm/s²)', rob.accel ? rob.accel.join(', ') : ''],
        ['Gyro (rad/s)', rob.gyro ? rob.gyro.join(', ') : ''],
        ['Proximity', rob.prox_mm !== undefined ? `${rob.prox_mm} mm` : ''],
        ['Touched', rob.touched === undefined ? '' : (rob.touched ? 'yes' : 'no')],
    ]);
    const linkRow = (name, l) => l ? [name, l.ok ? `ok (${l.ms} ms)` : l.detail, l.ok ? 'ok-text' : 'bad-text'] : [name, ''];
    renderKV('diagLinks', links.error ? [['Config', links.error, 'bad-text']] : [
        linkRow('Home Assistant', links.home_assistant),
        linkRow('Whisper', links.whisper),
        linkRow('MQTT', links.mqtt),
    ]);
    renderKV('diagServices', Object.entries(d.services || {}).map(([k, v]) =>
        [k, v, v === 'active' ? 'ok-text' : 'bad-text']));
    renderKV('diagSystem', [
        ['OS', sys.os_version],
        ['Uptime', duration(sys.uptime_s)],
        ['Load', (sys.load || []).join(' ')],
        ['CPU', sys.cpu_mhz ? `${sys.cpu_mhz} MHz, ${sys.cpu_temp_c} °C` : `${sys.cpu_temp_c} °C`, sys.cpu_temp_c > 70 ? 'warn-text' : ''],
        ['Memory free', sys.mem_available_mb !== undefined ? `${sys.mem_available_mb} / ${sys.mem_total_mb} MB` : ''],
        ['/data free', sys.disk_data ? `${sys.disk_data.free_mb} / ${sys.disk_data.total_mb} MB` : ''],
        ['System free', sys.disk_root ? `${sys.disk_root.free_mb} / ${sys.disk_root.total_mb} MB` : ''],
    ]);
    const dbm = parseInt(net.signal_dbm, 10);
    renderKV('diagNetwork', [
        ['IP', net.ip],
        ['Wi-Fi', net.ssid],
        ['Signal', net.signal_dbm ? `${net.signal_dbm} dBm` : '', dbm < -75 ? 'warn-text' : 'ok-text'],
        ['Access point', net.bssid],
        ['Frequency', net.freq ? `${net.freq} MHz` : ''],
        ['MAC', net.mac],
    ]);
    const rs = d.head_resync || {};
    document.getElementById('resyncInfo').textContent = rs.guard_stopped
        ? 'Head guard stopped: recalibrating didn\'t help, which points to a head encoder problem (cable or motor).'
        : rs.count
            ? `Head recalibrated ${rs.count}× since the UI started; last ${new Date(rs.at).toLocaleTimeString()} (${rs.reason}).`
            : 'Head guard active: recalibrates automatically if he thinks he is picked up while docked.';
    const voice = d.voice || [];
    document.getElementById('diagVoice').textContent = voice.length ? voice.join('\n') : 'No voice requests yet.';
}

async function loadLogs() {
    const unit = document.getElementById('logUnit').value;
    const out = document.getElementById('logOutput');
    out.textContent = 'Loading…';
    try {
        const res = await fetch(`/api/mods/Diagnostics/logs?unit=${encodeURIComponent(unit)}`);
        out.textContent = await res.text();
        out.scrollTop = out.scrollHeight;
    } catch (e) {
        out.textContent = `network error: ${e.message}`;
    }
}

setInterval(() => {
    const auto = document.getElementById('diagAuto');
    const visible = document.getElementById('overview').classList.contains('active');
    if (auto && auto.checked && visible && !document.hidden) loadDiagnostics();
}, 5000);

// Voice & Home Assistant config

function vectroStatus(msg, ok) {
    const el = document.getElementById('vectroStatus');
    el.hidden = !msg;
    el.className = 'banner ' + (ok ? 'banner-ok' : 'banner-bad');
    el.textContent = msg;
}

async function loadVectroConfig() {
    try {
        const res = await fetch('/api/mods/VectroConfig/get');
        const c = await res.json();
        const v = c.voice || {}, m = c.mqtt || {};
        document.getElementById('cfgHAURL').value = v.ha_url || '';
        document.getElementById('cfgHAToken').value = v.ha_token || '';
        document.getElementById('cfgAgent').value = v.agent || '';
        document.getElementById('cfgLanguage').value = v.language || '';
        document.getElementById('cfgWhisper').value = v.whisper || '';
        document.getElementById('cfgBroker').value = m.broker || '';
        document.getElementById('cfgMQTTUser').value = m.username || '';
        document.getElementById('cfgMQTTPass').value = m.password || '';
    } catch (e) {
        vectroStatus(`Couldn't load settings: ${e.message}`, false);
    }
}

async function saveVectroConfig(ev) {
    ev.preventDefault();
    const val = id => document.getElementById(id).value.trim();
    const body = {
        voice: {
            ha_url: val('cfgHAURL'), ha_token: val('cfgHAToken'), agent: val('cfgAgent'),
            language: val('cfgLanguage'), whisper: val('cfgWhisper'),
        },
        mqtt: { broker: val('cfgBroker'), username: val('cfgMQTTUser'), password: val('cfgMQTTPass') },
    };
    vectroStatus('Saving and restarting voice services…', true);
    try {
        const res = await fetch('/api/mods/VectroConfig/set', {
            method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
        });
        const r = await res.json();
        if (!res.ok) {
            vectroStatus(r.message || 'Save failed.', false);
            return;
        }
        vectroStatus('Saved. Voice services restarted.', true);
        loadVectroConfig();
    } catch (e) {
        vectroStatus(`network error: ${e.message}`, false);
    }
}

// Preferences: offer every IANA time zone the browser knows.

async function populateTimezones() {
    const sel = document.getElementById('timezone');
    let zones = [];
    try { zones = Intl.supportedValuesOf('timeZone'); } catch (e) { }
    if (!zones.length) {
        zones = ['Pacific/Honolulu', 'America/Anchorage', 'America/Los_Angeles', 'America/Denver',
            'America/Phoenix', 'America/Chicago', 'America/New_York', 'Europe/London', 'Europe/Paris',
            'Europe/Berlin', 'Asia/Tokyo', 'Australia/Sydney', 'UTC'];
    }
    sel.innerHTML = zones.map(z => `<option value="${esc(z)}">${esc(z.replace(/_/g, ' '))}</option>`).join('');
}

// Make sure a saved zone shows even if the browser's list lacks it.
function selectTimezone(zone) {
    const sel = document.getElementById('timezone');
    if (zone && ![...sel.options].some(o => o.value === zone)) {
        sel.add(new Option(zone, zone), 0);
    }
    sel.value = zone;
}

// Actions

function actionStatus(msg, ok) {
    const el = document.getElementById('actionStatus');
    el.hidden = !msg;
    el.className = 'banner ' + (ok ? 'banner-ok' : 'banner-bad');
    el.textContent = msg;
}

const actionLabels = {
    headResync: 'Sweeping head…', calibrateHead: 'Recalibrating: robot software is restarting, back in about a minute…', dock: 'Driving to the charger…', undock: 'Driving off the charger…',
    restartVoice: 'Restarting voice services…', restartRobot: 'Restarting robot software…', reboot: 'Rebooting…',
};

async function robotAction(action, confirmText, form) {
    if (confirmText && !confirm(confirmText)) return;
    const buttons = document.querySelectorAll('#overview .actions button, .say-row button');
    buttons.forEach(b => b.disabled = true);
    actionStatus(actionLabels[action] || 'Working…', true);
    try {
        const res = await fetch(`/api/mods/Actions/${action}`, { method: 'POST', body: form });
        const r = await res.json();
        actionStatus(res.ok ? 'Done.' : (r.message || 'Failed.'), res.ok);
    } catch (e) {
        actionStatus(`network error: ${e.message}`, false);
    } finally {
        buttons.forEach(b => b.disabled = false);
        loadDiagnostics();
    }
}

// Drive, camera and personality go through Vectro's vic-cloud, which
// shares one control session with Home Assistant.

async function robotPost(path, fields, label) {
    const body = new URLSearchParams(fields);
    actionStatus(label || 'Working…', true);
    try {
        const res = await fetch(`/api/mods/Robot/${path}`, { method: 'POST', body });
        const r = await res.json();
        actionStatus(res.ok ? 'Done.' : (r.message || 'Failed.'), res.ok);
        return res.ok;
    } catch (e) {
        actionStatus(`network error: ${e.message}`, false);
        return false;
    }
}

function robotCommand(id) {
    robotPost('command', { id }, `Running ${id.replace('_', ' ')}…`);
}

async function sayText(ev) {
    ev.preventDefault();
    const input = document.getElementById('sayInput');
    const text = input.value.trim();
    if (!text) return;
    if (await robotPost('say', { text }, 'Speaking…')) input.value = '';
}

function setRobotOption(path, selectId) {
    const name = document.getElementById(selectId).value;
    robotPost(path, { name }, `Setting ${path.replace('_', ' ')}…`);
}

async function takeSnapshot() {
    const img = document.getElementById('cameraImage');
    const hint = document.getElementById('cameraHint');
    hint.textContent = 'Capturing…';
    try {
        const res = await fetch('/api/mods/Robot/snapshot', { cache: 'no-store' });
        if (!res.ok) {
            const r = await res.json();
            hint.textContent = r.message || 'Snapshot failed.';
            return;
        }
        const blob = await res.blob();
        if (img.src) URL.revokeObjectURL(img.src);
        img.src = URL.createObjectURL(blob);
        img.hidden = false;
        hint.hidden = true;
    } catch (e) {
        hint.textContent = `network error: ${e.message}`;
    }
}

async function loadRobotOptions() {
    try {
        const res = await fetch('/api/mods/Robot/options');
        const o = await res.json();
        const fill = (id, list) => {
            document.getElementById(id).innerHTML = (list || []).map(v => `<option>${esc(v)}</option>`).join('');
        };
        fill('eyeColorSelect', o.eye_colors);
        fill('volumeSelect', o.volumes);
    } catch (e) { }
}
