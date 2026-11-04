-- A row means the user's Libredesk password follows their Kuraka password.
CREATE TABLE helpdesk_agents (
    helpdesk_id INTEGER NOT NULL REFERENCES helpdesks(id) ON DELETE CASCADE,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id INTEGER NOT NULL,
    PRIMARY KEY (helpdesk_id, user_id)
);

CREATE INDEX helpdesk_agents_user_id ON helpdesk_agents(user_id);
