function validateSensitivity(val) { return !(isNaN(val) || val < 0.001 || val > 0.999); }
async function setSensitivity() {
    const sld = document.getElementById('sensitivitySlider');
    const inp = document.getElementById('sensitivityInput');
    let txt = await (await fetch('/api/mods/SensitivityPV/get')).text();
    const v = parseFloat(txt);
    sld.value = v;
    inp.value = v;
}
async function sendSensitivity() {
    const raw = document.getElementById('sensitivityInput').value;
    const v = parseFloat(raw);
    if (!validateSensitivity(v)) return alert('value must be between 0.001 and 0.999');
    await fetch(`/api/mods/SensitivityPV/set?value=${v}`);
    console.log('sensitivity set to', v);
    RestartVic();
}

async function RestartVic() {
    const tabsEl = document.querySelector('.tabs');
    const activePanel = document.querySelector('.tab-content.active');
    tabsEl.style.display = 'none';
    if (activePanel) activePanel.classList.remove('active');
    show('showDuringVicRestart');
    await fetch('/api/extra/restartvic', { method: 'POST' });
    hide('showDuringVicRestart');
    tabsEl.style.display = 'flex';
    document.querySelectorAll('.tab-content').forEach(c => c.style.display = '');
    if (activePanel) activePanel.classList.add('active');
}