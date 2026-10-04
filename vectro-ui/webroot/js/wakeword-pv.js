function setWakeStatus(status) {
    const el = document.getElementById('wakeWordStatus');
    el.innerHTML = `<p>${status}</p>`;
}

async function genWakeWord() {
    const kw = document.getElementById('keyword').value;
    setWakeStatus('Generating wake word...');
    //['genWakeWord', 'keyword', 'revertDefaultWakeWord', 'keywordLabel'].forEach(hide);
    try {
        const res = await fetch(`/api/mods/WakeWordPV/request-model?keyword=${kw}`);
        if (!res.ok) {
            const e = await res.json();
            setWakeStatus(`${e.status}: ${e.message}`);
        } else {
            setWakeStatus('Wake word generated and installed. Restarting...');
            await RestartVic();
            setWakeStatus('Your new wake word is now implemented.');
        }
    } catch (e) {
        setWakeStatus(`network error: ${e.message}`);
    } finally {
        //['keyword', 'genWakeWord', 'revertDefaultWakeWord', 'keywordLabel'].forEach(show);
    }
}

async function revertDefaultWakeWord() {
    setWakeStatus('Deleting wake word...');
    // ['genWakeWord', 'keyword', 'revertDefaultWakeWord', 'keywordLabel'].forEach(hide);
    await fetch('/api/mods/WakeWordPV/delete-model');
    setWakeStatus('Custom model deleted. Restarting...');
    await RestartVic();
    setWakeStatus('Custom model deleted.');
    //['keyword', 'genWakeWord', 'keywordLabel', 'revertDefaultWakeWord'].forEach(show);
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