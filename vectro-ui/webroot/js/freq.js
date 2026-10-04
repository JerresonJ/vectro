async function FreqChange_Submit() {
    hide('mods');
    SetModStatus('FreqChange is applying, please wait...');
    const freq = document.querySelector('input[name="frequency"]:checked').value;
    try {
        const res = await fetch(`/api/mods/FreqChange/set?freq=${freq}`);
        if (!res.ok) {
            const e = await res.json();
            SetModStatus(`FreqChange failed: ${e.message}`);
        } else {
            HideModStatus();
        }
    } catch {
        SetModStatus('FreqChange failed.');
    } finally {
        show('mods');
    }
}