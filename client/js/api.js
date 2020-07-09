import { document, Quill, WebSocket, signalError, setTimeout} from "./externals.js";
const Delta = Quill.import("delta");

const LAYOUT_ID = 0;
const INITIAL_BACKOFF_MS = 1000;

const STATE_DISCONNECTED = 0;
const STATE_CONNECTED = 1;
const STATE_DONE = 2;

class Connection {
  constructor(onchange) {
    this.onchange = onchange;
    this.clientId = null;
    this.sessionId = null;
    this.ws = null;
    this.state = STATE_DISCONNECTED;
  }

  connect() {
    // Only allow one socket connection at a time.
    this.close();
    const ws = this.ws = new WebSocket("ws://" + document.location.host + "/ws");
    ws.onclose = ws.onerror = () => {
      console.log("Disconnected!");
      this.closeAndReconnect();
    };
    // TODO: session negotiation timeout
    ws.onmessage = (event) => {
      const message = JSON.parse(event.data);
      if (this.state === STATE_DONE) {
        this.onchange(message);
      } else if (this.state === STATE_CONNECTED) {
        if ((!message.session) || (!message.client)) {
          console.log("Invalid init response:", message);
          this.closeAndReconnect();
          return;
        }
        if (this.clientId && this.sessionId &&
            (this.clientId != message.client ||
             this.sessionId != message.session)) {
          signalError("TODO: handle session change");
        }
        console.log("Initialized!")
        this.clientId = message.client;
        this.sessionId = message.session;
        this.state = STATE_DONE;
      }
    };
    ws.onopen = (event) => {
      console.log("Connected!");
      this.state = STATE_CONNECTED;
      this.reconnectWait = INITIAL_BACKOFF_MS;
      ws.send(JSON.stringify({
        session: this.sessionId,
        client: this.clientId,
      }));
    };
  }

  closeAndReconnect() {
    this.close();
    setTimeout(() => this.connect(), this.reconnectWait);
    this.reconnectWait = this.reconnectWait * 2;
  }

  close() {
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
    this.state = STATE_DISCONNECTED;
  }

  send(message) {
    if (this.state !== STATE_DONE) {
      signalError("Not connected!");
      return;
    }
    /* All communication is asynchronous, no need to get a response here */
    this.ws.send(JSON.stringify(message));
  }
}

export class Api {
  constructor() {
    this.inflight_changes = {};
    this.buffered_changes = {};
    this.acks = {};
    this.last = {};
  }

  init(layout, onchange, onverify) {
    this.layout = layout;
    this.onchange = onchange;
    this.onverify = onverify;
    this.connection = new Connection(({updates, hashes}) => {
      for (const [id, update] of Object.entries(updates || {})) {
        const ack = this.acks[id] || 0;
        if (ack !== update.base) {
          console.log(`Base mismatch for file ${id}, local: ${ack}, remote: ${update.base}`);
          delete updates[id];
          continue;
        }
        if (this.inflight_changes[id]) {
          if (update.last_committed_client_version >= this.inflight_changes[id].client_version) {
            delete this.inflight_changes[id];
            this.last[id] = update.last_committed_client_version;
          }
        }
        if (this.buffered_changes[id]) {
          const localDelta = this.buffered_changes[id].delta;
          const remoteDelta = new Delta(update.delta);
          // Remote delta is applied first
          const newLocalDelta = remoteDelta.transform(localDelta, true);
          const newRemoteDelta = localDelta.transform(remoteDelta, false);
          this.buffered_changes[id] = {
            delta: newLocalDelta,
            base: update.version,
          };
          update.delta = newRemoteDelta;
          updates[id] = update;
        }
      }
      const editorData = {};
      const layoutUpdate = updates[LAYOUT_ID];
      if (layoutUpdate) {
        this.layout.update(layoutUpdate);
        editorData["layout"] = {
          columns: this.layout.columns
        };
        this.acks[LAYOUT_ID] = layoutUpdate.version;
      }
      const knownIds = this.layout.knownIds();
      const knownUpdates = Object.values(updates).filter(update => knownIds.includes(update.id));
      editorData.rows = knownUpdates;
      let ackChanged = false;
      for (const update of knownUpdates) {
        ackChanged = ackChanged || (this.acks[update.id] !== update.version);
        this.acks[update.id] = update.version;
      }
      this.onchange(editorData);
      if (hashes) {
        if (hashes[LAYOUT_ID]) {
          this.layout.verify(hashes[LAYOUT_ID]);
          delete hashes[LAYOUT_ID];
        }
        this.onverify(hashes);
      }
      if (ackChanged) {
        this.action(null);
      }
    });
    this.connection.connect();
  }

  textchange(id, delta, base) {
    if (id === LAYOUT_ID) {
      signalError("Layout file should not be changed from client side!");
      return;
    }
    let changed = !this.buffered_changes[id];
    this.buffered_changes[id] = this.buffered_changes[id] || { id: id, delta: new Delta(), base: base };
    if (this.buffered_changes[id].base !== base) {
      signalError("Base mismatch, something is wrong!");
      return;
    }
    const aggregated = this.buffered_changes[id].delta.compose(delta);
    if (aggregated.ops.length === 0) {
      changed = true;
      delete this.buffered_changes[id];
    } else {
      this.buffered_changes[id].delta = aggregated;
    }
    if (changed) {
      this.onchange({
        dirtyChanges: {
          [id]: !!this.buffered_changes[id]
        }
      });
    }
  }

  action(data) {
    if (data) {
      if (data.command === "Newcol") {
        this.layout.createColumn();
        this.onchange({layout: { columns: this.layout.columns }});
        data = null;
      } else if (data.command === "Delcol") {
        this.layout.removeColumn(data.id - 1 + data.id % 2);
        this.onchange({layout: { columns: this.layout.columns }});
        data = null;
      }
    }
    // Move all possible buffered changes into inflight changes
    for (const { id, delta, base } of Object.values(this.buffered_changes)) {
      if (!this.inflight_changes[id]) {
        this.inflight_changes[id] = {
          id,
          delta,
          base,
          client_version: (this.last[id] || 0) + 1,
        };
        delete this.buffered_changes[id];
      }
    }
    const hasLocalChanges = Object.keys(this.buffered_changes).length > 0;
    const payload = {
      action: hasLocalChanges ? null : data,
      changes: Object.values(this.inflight_changes),
      acks: this.acks,
    };
    const dirtyChanges = {};
    Object.keys(this.inflight_changes).forEach(id => {
      dirtyChanges[id] = false;
    });
    this.onchange({ dirtyChanges });
    this.connection.send(payload);
    if (data && hasLocalChanges) {
      signalError("There is still local changes not synced! Please wait for a while");
    }
  }

  move({id, x, y}) {
    this.layout.move(id, x, y);
    this.onchange({ layout: { columns: this.layout.columns } });
  }

  sizechange(sizes) {
    this.layout.updateSizes(sizes);
  }
}
