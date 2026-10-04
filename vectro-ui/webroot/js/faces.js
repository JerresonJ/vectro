function facesSetStatus(msg, isError) {
    const el = document.getElementById('facesStatus');
    if (!msg) {
        el.style.display = 'none';
        el.textContent = '';
        el.className = 'faces-status';
        return;
    }
    el.style.display = 'block';
    el.textContent = msg;
    el.className = 'faces-status ' + (isError ? 'error' : 'ok');
}

async function facesFetchText(url, opts) {
    const res = await fetch(url, opts);
    const txt = await res.text();
    if (!res.ok) {
        throw new Error(txt || `http ${res.status}`);
    }
    return txt;
}

async function facesFetchJson(url, opts) {
    const res = await fetch(url, opts);
    const txt = await res.text();
    if (!res.ok) throw new Error(txt || `http ${res.status}`);
    try { return JSON.parse(txt); }
    catch { throw new Error('bad json from server'); }
}

async function facesRefresh() {
    facesSetStatus('', false);
    const listEl = document.getElementById('facesList');
    listEl.innerHTML = '<p>Loading...</p>';

    try {
        const faces = await facesFetchJson('/api/mods/Faces/getFaces');

        if (!Array.isArray(faces) || faces.length === 0) {
            listEl.innerHTML = '<p>No enrolled faces found.</p>';
            return;
        }

        const rows = faces.map(f => {
            const id = Number(f.id);
            const name = f.name;
            const age = Number(f.secondssincefirstenrolled);

            return `
        <div class="face-card">
            <div class="face-row">
                <div>
                    <div><b>${name || '(unnamed)'}</b></div>
                    <div class="face-meta">
                        id: ${id} - seconds since first enrolled: ${age}
                    </div>
                </div>
                <div class="face-actions">
                    <button type="button"
                        onclick="facePromptRename(${id}, '${name.replaceAll("'", "\\'")}')">
                        Rename
                    </button>
                    <button type="button" onclick="faceDelete(${id})">
                        Delete
                    </button>
                </div>
            </div>
        </div>
    `;
        }).join('');


        listEl.innerHTML = rows;
    } catch (e) {
        listEl.innerHTML = '';
        facesSetStatus(`failed to load faces: ${e.message}`, true);
    }
}

async function faceDelete(id) {
    facesSetStatus('', false);
    try {
        const qs = new URLSearchParams({ id: String(id) });
        await facesFetchText(`/api/mods/Faces/deleteFace?${qs.toString()}`);
        facesSetStatus('Deleted.', false);
        facesRefresh();
    } catch (e) {
        facesSetStatus(`Delete failed: ${e.message}`, true);
    }
}

async function facePromptRename(id, currentName) {
    facesSetStatus('', false);
    const newName = prompt(`Rename face id ${id}`, currentName || '');
    if (newName === null) return;

    const trimmed = newName.trim();
    if (!trimmed) {
        facesSetStatus('No name given.', true);
        return;
    }

    try {
        const qs = new URLSearchParams({ id: String(id), name: trimmed });
        await facesFetchText(`/api/mods/Faces/renameFace?${qs.toString()}`);
        facesSetStatus('Renamed.', false);
        facesRefresh();
    } catch (e) {
        facesSetStatus(`Rename failed: ${e.message}`, true);
    }
}

async function faceTrain() {
    facesSetStatus('', false);
    const input = document.getElementById('faceNewName');
    const name = (input.value || '').trim();
    if (!name) {
        facesSetStatus('Enter a name first.', true);
        return;
    }

    try {
        const qs = new URLSearchParams({ name });
        await facesFetchText(`/api/mods/Faces/trainFace?${qs.toString()}`);
        facesSetStatus('Enroll started. Look at Vector.', false);
        input.value = '';
        facesRefresh();
    } catch (e) {
        facesSetStatus(`train failed: ${e.message}`, true);
    }
}
