import { document, Quill, WebSocket, signalError, setTimeout} from "./externals.js";
const Delta = Quill.import("delta");

const LAYOUT_ID = 0;

const INITIAL_BACKOFF_MS = 1000;
class Connection {
  constructor(onchange) {
    this.onchange = onchange;
  }

  connect() {
    const self = this;
    const ws = this.ws = new WebSocket("ws://" + document.location.host + "/ws");
    ws.onclose = ws.onerror = function() {
      /* TODO: Add reconnect logic once we have state syncing in place. */
      console.log("Disconnected!");
      self.connected = false;
      /* console.log(`Connection closed, reconnect in ${self.backoff} milliseconds`);
       * setTimeout(function () {
       *   self.connect();
       * }, self.backoff);
       * self.backoff = self.backoff * 2; */
    };
    ws.onmessage = function (event) {
      self.onchange(JSON.parse(event.data));
    }
    ws.onopen = function (event) {
      console.log("Connection initiated!");
      self.connected = true;
      self.backoff = INITIAL_BACKOFF_MS;
    }
  }

  send(message) {
    if (!this.connected) {
      signalError("Not connected!");
      return;
    }
    /* All communication is asynchronous, no need to get a response here */
    this.ws.send(JSON.stringify(message));
  }
}

export class Api {
  constructor() {
    this.buffered_changes = {};
  }

  init(layout, onchange, onverify) {
    this.layout = layout;
    this.onchange = onchange;
    this.onverify = onverify;
    this.connection = new Connection(({changes, hashes}) => {
      changes = changes || [];
      const editorChanges = {};
      if (Object.keys(this.buffered_changes).length !== 0) {
        changes = changes.map(change => {
          const localDelta = this.buffered_changes[change.id];
          const remoteDelta = change.change.delta;
          if (localDelta && remoteDelta) {
            // TODO: verify the logic here later
            const ld = localDelta.change.delta;
            const rd = new Delta(change.change.delta);
            this.buffered_changes[change.id].change.delta = rd.transform(ld, true);
            this.buffered_changes[change.id].change.version = change.change.version;
            change.change.delta = ld.transform(rd, false);
          }
          return change;
        });
      }
      editorChanges.rows = changes.filter(change => change.id != LAYOUT_ID);
      const layoutChanges = changes.filter(change => change.id === LAYOUT_ID);
      if (layoutChanges.length > 0) {
        this.layout.update(layoutChanges);
        editorChanges["layout"] = {
          columns: this.layout.columns
        };
      }
      this.onchange(editorChanges);
      if (hashes) {
        if (hashes[LAYOUT_ID]) {
          this.layout.verify(hashes[LAYOUT_ID]);
          delete hashes[LAYOUT_ID];
        }
        this.onverify(hashes);
      }
    });
    this.connection.connect();
  }

  textchange(id, delta, version) {
    let changed = !this.buffered_changes[id];
    this.buffered_changes[id] = this.buffered_changes[id] || { id: id, change: { version: version } };
    if (this.buffered_changes[id].change.version !== version) {
      signalError("Version mismatch, something is wrong!");
      return;
    }
    const aggregated = (this.buffered_changes[id].change.delta || new Delta()).compose(delta);
    if (aggregated.ops.length === 0) {
      changed = true;
      delete this.buffered_changes[id];
    } else {
      this.buffered_changes[id].change.delta = aggregated;
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
    const d = this.layout.generateSizeChange();
    if (d) {
      this.textchange(LAYOUT_ID, d, this.layout.version);
    }
    const payload = {
      action: data,
      changes: Object.values(this.buffered_changes)
    };
    const dirtyChanges = {};
    Object.keys(this.buffered_changes).forEach(id => {
      dirtyChanges[id] = false;
    });
    // TODO: do we need to wait for ack?
    this.buffered_changes = {};
    this.onchange({ dirtyChanges });
    this.connection.send(payload);
  }

  move({id, x, y}) {
    this.layout.move(id, x, y);
    this.onchange({ layout: { columns: this.layout.columns } });
  }

  sizechange(sizes) {
    this.layout.updateSizes(sizes);
  }
}
