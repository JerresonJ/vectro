function setJdocStatus(status) {
    const el = document.getElementById('jdocStatus');
    el.innerHTML = `<p>${status}</p>`;
}

async function setLocation() {
    const v = document.getElementById('location').value;
    setJdocStatus("Setting location...")
    try {
        const res = await fetch(`/api/mods/JdocSettings/setLocation?location=${encodeURIComponent(v)}`);
        if (!res.ok) {
            const e = await res.json();
            setJdocStatus(`${e.status}: ${e.message}`);
        } else {
            getLocation()
            setJdocStatus('Successfully set location.');
        }
    } catch (e) {
        setJdocStatus(`network error: ${e.message}`);
    }
}

async function setTimezone() {
    const v = document.getElementById('timezone').value;
    setJdocStatus("Setting time zone...")
    try {
        const res = await fetch(`/api/mods/JdocSettings/setTimezone?timezone=${encodeURIComponent(v)}`);
        if (!res.ok) {
            const e = await res.json();
            setJdocStatus(`${e.status}: ${e.message}`);
        } else {
            getTimezone()
            setJdocStatus('Successfully set time zone.');
        }
    } catch (e) {
        setJdocStatus(`network error: ${e.message}`);
    }
}

async function setTempUnits() {
    const v = document.getElementById('tUnits').value;
    setJdocStatus("Setting temperature units...")
    try {
        const res = await fetch(`/api/mods/JdocSettings/setFahrenheit?temp=${encodeURIComponent(v)}`);
        if (!res.ok) {
            const e = await res.json();
            setJdocStatus(`${e.status}: ${e.message}`);
        } else {
            getTempUnits()
            setJdocStatus('Successfully set temp units.');
        }
    } catch (e) {
        setJdocStatus(`network error: ${e.message}`);
    }
}

async function getLocation() {
    try {
        const res = await fetch(`/api/mods/JdocSettings/getLocation`);
        if (!res.ok) {
            const e = await res.json();
            console.log(`${e.status}: ${e.message}`);
        } else {
            const e = await res.text();
            document.getElementById('location').value = e;
        }
    } catch (e) {
        console.log(`network error: ${e.message}`);
    }
}

async function getTimezone() {
    try {
        const res = await fetch(`/api/mods/JdocSettings/getTimezone`);
        if (!res.ok) {
            const e = await res.json();
            console.log(`${e.status}: ${e.message}`);
        } else {
            const e = await res.text();
            selectTimezone(e);
        }
    } catch (e) {
        console.log(`network error: ${e.message}`);
    }
}

async function getTempUnits() {
    try {
        const res = await fetch(`/api/mods/JdocSettings/getFahrenheit`);
        if (!res.ok) {
            const e = await res.json();
            console.log(`${e.status}: ${e.message}`);
        } else {
            const e = await res.text();
            document.getElementById('tUnits').value = e;
        }
    } catch (e) {
        console.log(`network error: ${e.message}`);
    }
}