
function getEmail() {
    return process.env.USER_EMAIL;
}
async function listIncidents(client) {
    try {
        const resp = await client.get('/incidents');
        return resp.resource;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

async function getIncident(client, id) {
    try {
        const resp = await client.get(`/incidents/${id}`);
        return resp.data;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

async function updateIncidentStatus(client, id, status) {
    try {
        const orig = await getIncident(client, id);
        console.log("Original: ", orig.incident.id);
        const resp = await client.put(`/incidents/${orig.incident.id}`, {
            headers: {
                Accept: 'application/vnd.pagerduty+json;version=2',
                From: getEmail(),
            },
            data: {
                incident: {
                    type: 'incident_reference',
                    status: status
                }
            },
        });
        return resp.data;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

async function listIncidentNotes(client, id) {
    try {
        const resp = await client.get(`/incidents/${id}/notes`);
        return resp.resource;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

async function addIncidentNote(client, id, contents) {
    try {
        const resp = await client.post(`/incidents/${id}/notes`, {
            headers: {
                From: getEmail(),
            },
            data: {
                note: {
                    content: contents
                }
            }
        });
        return resp.data;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

async function listIncidentAlerts(client, id) {
    try {
        const resp = await client.get(`/incidents/${id}/alerts`);
        return resp.data;
    } catch (error) {
        console.error(error);
        throw error;
    }
}

module.exports = {
    listIncidents,
    getIncident,
    updateIncidentStatus,
    addIncidentNote,
    listIncidentNotes,
    listIncidentAlerts
}