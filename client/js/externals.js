export const document = window.document;
export const redom = window.redom;
export const Quill = window.Quill;
export const WebSocket = window.WebSocket;
export const addEventListener = window.addEventListener;
export const getComputedStyle = window.getComputedStyle;
export const signalError = window.alert;
export const setTimeout = window.setTimeout;

const subtle = window.crypto && window.crypto.subtle;

export function verifyContent(delta, localVersion, { hash, version }) {
  if (!subtle) {
    console.log("Hash provided but subtle crypto is missing, maybe checking the browser again?");
    return;
  }
  if (localVersion !== version) {
    console.log("Hash provided is for a different version, skipping validation");
    return;
  }
  const text = delta.filter(op => typeof op.insert === "string")
                    .map(op => op.insert)
                    .join("");
  const encodedText = new TextEncoder().encode(text);
  subtle.digest("SHA-256", encodedText)
    .then(hashBuffer => {
      const hashArray = Array.from(new Uint8Array(hashBuffer));
      const hashHex = hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
      if (hashHex !== hash) {
        signalError("Error verifying content, expected hash: " +
                    hash + " actual hash: " + hashHex);
      }
    })
    .catch(e => {
      console.log("Digest generation error: " + e);
    });
}

export const clipboard = window.navigator.clipboard || {
  writeText: (_content) => {
    return Promise.reject(new Error("Clipboard is not available!"));
  },
  readText: () => {
    return Promise.reject(new Error("Clipboard is not available!"));
  }
};

export const IS_MOBILE = /Android|iPhone|iPad/i.test(window.navigator.userAgent);

export function windowDimension() {
  return {
    width: window.innerWidth,
    height: window.innerWidth
  };
}
