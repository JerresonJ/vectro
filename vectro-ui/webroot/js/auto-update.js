async function setAutoUpdateStatus(status) {
    const el = document.getElementById('autoUpdateStatus');
    el.innerHTML = `<p>${status}</p>`;
    show('autoUpdateStatus');
}

async function checkAutoUpdateStatus() {
    ['autoUpdateStatus', 'autoUpdateInhibit', 'autoUpdateAllow'].forEach(hide);
    let res = await fetch('/api/mods/AutoUpdate/isSelfMadeBuild');
    let txt = await res.text();
    if (txt.includes('true')) {
        setAutoUpdateStatus('This is a self-made build. This build cannot auto-update.');
    } else {
        res = await fetch('/api/mods/AutoUpdate/isInhibitedByUser');
        txt = await res.text();
        if (txt.includes('true')) {
            setAutoUpdateStatus('Auto-updates: not enabled');
            show('autoUpdateAllow');
        } else {
            setAutoUpdateStatus('Auto-updates: enabled');
            show('autoUpdateInhibit');
        }
    }
}

async function autoUpdateInhibit() {
    ['autoUpdateStatus', 'autoUpdateInhibit', 'autoUpdateAllow'].forEach(hide);
    await fetch('/api/mods/AutoUpdate/setInhibited');
    checkAutoUpdateStatus();
}
async function autoUpdateAllow() {
    ['autoUpdateStatus', 'autoUpdateInhibit', 'autoUpdateAllow'].forEach(hide);
    await fetch('/api/mods/AutoUpdate/setAllowed');
    checkAutoUpdateStatus();
}