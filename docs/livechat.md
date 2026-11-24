# Live chat

How a live chat created in Kuraka (domain page, **Add live chat**) behaves, and what to set in Kuraka and in the helpdesk (Libredesk). Builds on [integrations.md](integrations.md).

## The visitor's path

1. The visitor opens the chat. If the **options menu** is on, they see the welcome text, one button per menu item and a **Talk to an agent** button.
2. A menu button shows its answer in the chat. Nothing reaches the helpdesk.
3. **Talk to an agent** checks availability (when an offline mode is set), then shows the **form** (or the message box if the form is off).
4. The first message creates the conversation in the helpdesk. With a **team** set, it goes to that team and an agent gets it automatically.
5. The chat ends when the visitor or an agent ends it, or after **inactivity**.

## Form before the chat

The name is always asked and required. Last name, email and phone can each be shown and made required; new chats show email and phone as optional.

The last name is a contact custom attribute in Libredesk (key `last_name`). Kuraka creates it the first time a chat shows the field, and it appears on the contact in the helpdesk. Fields added in Libredesk (other custom attributes) are kept and shown after Kuraka's fields.

## Menu and templates

Up to 8 buttons, each with a text (up to 60 characters) and an answer (up to 1000). The text of the agent button is optional.

Three templates fill the menu in the panel language: **Online store** (orders, shipping, exchanges and returns, payments), **Services** (prices, appointments, opening hours) and **Support** (technical problem, billing, other question). A template replaces the current buttons; edit the answers to point to your own pages before saving.

## Team and automatic assignment

**Team that answers** lists the helpdesk's teams. New conversations of the chat are assigned to that team, and Libredesk shares them among the team's online agents.

- With no team, nobody gets the chats automatically: agents pick them from the inbox. The panel warns about it.
- A team with no agents gets a warning too. Add agents in Libredesk, **Admin → Teams**.
- A team set to **Manual** assignment gets a warning: nobody gets its chats automatically. Kuraka never changes an existing team, as its assignment also applies to the team's emails.
- **Create a team for this chat** creates a team with **Round robin** assignment (named "Chat <chat name>" by default) and selects it. Add its agents in Libredesk, **Admin → Teams**. Use a team only for this chat.

## Business hours and offline modes

An agent is available when it is within business hours and at least one agent (of the team, if set) is online. Business hours come from the team, else from the helpdesk's general settings (**Admin → Business hours**); with none configured, it is always within hours.

**When no agent is available:**

| Mode | What the visitor sees |
|---|---|
| Do not check | The chat works as usual, no availability check. Chats created before this setting existed use it. |
| Keep the message waiting (default) | A notice, then the form. The conversation is created with status **Waiting**. |
| Let visitors choose | A notice and two buttons: wait for an agent, or email us. |
| Only offer email | A notice and an **Email us** button. No conversation is created. |

The last two need the email address visitors write to.

## Waiting status

A conversation created while no agent is available has status **Waiting** (Spanish: "En espera"). It becomes **Open** as soon as an agent is assigned to it or replies. Filter by this status to answer pending chats when you come back. Inactivity never closes a Waiting conversation.

## Ending a chat

- The visitor can press **End chat** in the widget.
- When an agent sets the conversation to **Resolved** or **Closed**, the widget shows that the chat has ended and offers to start a new one. Visitors cannot reply to an ended chat (new chats set this for visitors and signed-in users).
- **Close after inactivity (minutes):** when the visitor does not answer the agent's last message for this long, the chat is closed. New chats use 30 minutes; 0 turns it off.

## Hiding agent names

On by default for new chats. The widget shows the chat name and logo instead of the agent's name and photo, and the helpdesk does not send them to the widget.

## Automating more

The settings above cover the common case. For more, use Libredesk directly:

- **Automation rules** (**Admin → Automations**, on new conversations): set priority, tags or a different team based on the inbox, the contact or the message text.
- **Webhooks** (**Admin → Webhooks**): the `conversation.created` event lets another system react to each new chat, for example to notify a phone or a CRM.
- **AI agent** (**Admin → AI assistants**): an AI assistant can answer first and hand the conversation to a human team when it cannot help. Set the same team here so the handoff lands with the right agents.
