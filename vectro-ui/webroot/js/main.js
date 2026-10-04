const tabs = document.querySelectorAll('.tabs button');

function showTab(target) {
    const btn = document.querySelector(`.tabs button[data-target="${target}"]`);
    if (!btn) return;
    tabs.forEach(b => b.classList.remove('active'));
    btn.classList.add('active');
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    document.querySelector(target).classList.add('active');
    try { localStorage.setItem('vectroTab', target); } catch (e) { }
}

tabs.forEach(btn => btn.addEventListener('click', () => showTab(btn.dataset.target)));

try {
    const saved = localStorage.getItem('vectroTab');
    if (saved) showTab(saved);
} catch (e) { }

function hide(id) { document.getElementById(id).style.display = 'none'; }
function show(id) { document.getElementById(id).style.display = 'block'; }

async function GetCurrent(mod) {
    let res = await fetch(`/api/mods/${mod}/get`);
    return res.text();
}

function SetModStatus(msg) {
    const div = document.getElementById('modStatus');
    div.innerHTML = `<p>${msg}</p>`;
    div.style.display = msg ? 'block' : 'none';
}

function HideModStatus() { hide('modStatus'); }

async function UpdateAllMods() {
    hide('restartNeeded');
    hide('showDuringVicRestart');

    const data = await GetCurrent('FreqChange');
    document.getElementsByName('frequency')
        .forEach(rb => { if (rb.value == data) rb.checked = true; });
    checkAutoUpdateStatus();
    setSensitivity();
    await populateTimezones();
    getTimezone();
    getLocation();
    getTempUnits();
    facesRefresh();
    loadVectroConfig();
    loadDiagnostics();
    loadRobotOptions();
}

window.addEventListener('DOMContentLoaded', UpdateAllMods);
