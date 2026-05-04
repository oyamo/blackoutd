const map = L.map('map', {
    zoomControl: false
}).setView([-1.2921, 36.8219], 6);

L.tileLayer('https://{s}.basemaps.cartocdn.com/dark_all/{z}/{x}/{y}{r}.png', {
    attribution: '&copy; CartoDB',
    subdomains: 'abcd',
    maxZoom: 19
}).addTo(map);

L.control.zoom({
    position: 'bottomleft'
}).addTo(map);

const markerIcon = L.divIcon({
    className: 'custom-marker',
    html: `
        <div style="
            width: 14px; 
            height: 14px; 
            background: #f59e0b; 
            border-radius: 50%;
            border: 2px solid white;
            box-shadow: 0 0 16px #f59e0b;
        "></div>
    `,
    iconSize: [14, 14],
    iconAnchor: [7, 7]
});

const roadStyle = {
    color: '#f59e0b',
    weight: 4,
    opacity: 0.8,
    dashArray: '5, 10',
    lineCap: 'round',
    lineJoin: 'round',
    className: 'glowing-line'
};

let globalData = [];
let mapLayers = [];
let heatLayer = null;

async function init() {
    try {
        const response = await fetch('/api/blackouts');
        globalData = await response.json();

        applyFilters();
    } catch (e) {
        document.getElementById('blackoutList').innerHTML = `<div class="loader" style="color:red">API Error</div>`;
    }
}

function updateStats(data) {
    let zones = 0;
    let counties = new Set();

    data.forEach(notice => {
        counties.add(notice.county);
        if (notice.detailed) zones += notice.detailed.length;
    });

    document.getElementById('totalZones').textContent = zones;
    document.getElementById('totalCounties').textContent = counties.size;
}

function renderMap(data) {
    const vizType = document.getElementById('mapType').value;

    mapLayers.forEach(l => map.removeLayer(l));
    mapLayers = [];
    if (heatLayer) {
        map.removeLayer(heatLayer);
        heatLayer = null;
    }

    const bounds = [];
    const heatPoints = [];

    data.forEach(notice => {
        if (!notice.detailed) return;

        notice.detailed.forEach(detail => {
            if (!detail.coordinates || detail.coordinates.length === 0) return;

            const popupHtml = `
                <div>
                    <div class="popup-title">${detail.location}</div>
                    <div class="popup-p"><strong>Region:</strong> ${notice.region}</div>
                    <div class="popup-p"><strong>County:</strong> ${notice.county}</div>
                    <div class="popup-p" style="color:#f59e0b; margin-top:8px;">${notice.date}</div>
                    <div class="popup-p">${notice.time}</div>
                    <div class="popup-p" style="font-size:10px; opacity:0.6; margin-top: 10px;">Source: ${detail.coordinates[0].source}</div>
                </div>
            `;

            if (vizType === 'heatmap') {
                detail.coordinates.forEach(c => {
                    if (c.lat !== 0 && c.long !== 0) {
                        heatPoints.push([c.lat, c.long, 0.4]);
                        bounds.push([c.lat, c.long]);
                    }
                });
            } else {
                if (detail.coordinates.length === 1) {
                    const coord = detail.coordinates[0];
                    if (coord.lat === 0 && coord.long === 0) return;

                    const marker = L.marker([coord.lat, coord.long], { icon: markerIcon })
                        .bindPopup(popupHtml)
                        .addTo(map);

                    mapLayers.push(marker);
                    bounds.push([coord.lat, coord.long]);
                } else {
                    const latlngs = detail.coordinates.map(c => [c.lat, c.long]);
                    const polyline = L.polyline(latlngs, roadStyle)
                        .bindPopup(popupHtml)
                        .addTo(map);

                    mapLayers.push(polyline);
                    latlngs.forEach(ll => bounds.push(ll));
                }
            }
        });
    });

    if (vizType === 'heatmap' && heatPoints.length > 0) {
        heatLayer = L.heatLayer(heatPoints, {
            radius: 40,
            blur: 30,
            maxZoom: 10,
            gradient: {
                0.2: '#0ea5e9',
                0.4: '#10b981',
                0.6: '#eab308',
                0.8: '#f97316',
                1.0: '#ef4444'
            }
        }).addTo(map);
    }

    if (bounds.length > 0) {
        map.fitBounds(bounds, { padding: [50, 50] });
    }
}

function renderSidebar(data) {
    const list = document.getElementById('blackoutList');
    list.innerHTML = '';

    if (data.length === 0) {
        list.innerHTML = `<div style="text-align:center; color: var(--text-muted); margin-top: 30px;">No blackout schedules found.</div>`;
        return;
    }

    data.forEach(notice => {
        const li = document.createElement('li');
        li.className = 'blackout-item';

        let subAreasStr = "";
        if (notice.detailed && notice.detailed.length > 0) {
            subAreasStr = notice.detailed.slice(0, 3).map(d => d.location).join(", ");
            if (notice.detailed.length > 3) subAreasStr += " and more...";
        }

        li.innerHTML = `
            <div class="item-header">
                <span class="county-tag">${notice.county}</span>
                <span style="font-size:11px; opacity:0.7;">${notice.date}</span>
            </div>
            <div class="item-title">${notice.area}</div>
            <div class="item-meta">
                <svg width="12" height="12" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z"></path></svg>
                ${notice.time}
            </div>
            <div style="font-size:12px; margin-top:8px; color:rgba(255,255,255,0.6)">
                ${subAreasStr}
            </div>
        `;

        li.addEventListener('click', () => {
            if (notice.detailed && notice.detailed.length > 0 && notice.detailed[0].coordinates.length > 0) {
                const bestCoord = notice.detailed[0].coordinates[0];
                if (bestCoord.lat !== 0) {
                    map.flyTo([bestCoord.lat, bestCoord.long], 13, {
                        duration: 1.5
                    });
                }
            }
        });

        list.appendChild(li);
    });
}

function parseInputDateToKplc(dateStr) {
    if (!dateStr) return "";
    const parts = dateStr.split("-");
    if (parts.length !== 3) return "";
    return `${parts[2]}.${parts[1]}.${parts[0]}`;
}

function applyFilters() {
    const query = document.getElementById('search').value.toLowerCase();
    const dateQuery = document.getElementById('dateFilter').value;
    const targetDateStr = dateQuery ? parseInputDateToKplc(dateQuery) : null;

    const filtered = globalData.filter(d => {
        const matchesQuery = d.area.toLowerCase().includes(query) || d.county.toLowerCase().includes(query);
        const matchesDate = targetDateStr ? d.date.includes(targetDateStr) : true;

        return matchesQuery && matchesDate;
    });

    updateStats(filtered);
    renderSidebar(filtered);
    renderMap(filtered);
}

document.getElementById('search').addEventListener('input', applyFilters);
document.getElementById('dateFilter').addEventListener('change', applyFilters);
document.getElementById('mapType').addEventListener('change', () => {
    const query = document.getElementById('search').value.toLowerCase();
    const dateQuery = document.getElementById('dateFilter').value;
    const targetDateStr = dateQuery ? parseInputDateToKplc(dateQuery) : null;

    const filtered = globalData.filter(d => {
        const matchesQuery = d.area.toLowerCase().includes(query) || d.county.toLowerCase().includes(query);
        const matchesDate = targetDateStr ? d.date.includes(targetDateStr) : true;
        return matchesQuery && matchesDate;
    });

    renderMap(filtered);
});

init();
