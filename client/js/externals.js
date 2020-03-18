export const document = window.document;
export const redom = window.redom;
export const Quill = window.Quill;
export const signalError = window.alert;

const subtle = window.crypto && window.crypto.subtle;

export function verifyContent(delta, hash) {
  if (!subtle) {
    console.log("Hash provided but subtle crypto is missing, maybe checking the browser again?");
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
