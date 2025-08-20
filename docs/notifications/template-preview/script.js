let wasmLoaded = false;

const updatePreview = () => {
  if (!wasmLoaded) return;
  const form = document.querySelector("#tplprev");
  const input = form.template.value;
  console.log("Input: %o", input);
  const arrFromCount = (key) =>
    Array.from(Array(form[key]?.valueAsNumber ?? 0), () => key);
  const states =
    form.report.value === "yes"
      ? [
          ...arrFromCount("skipped"),
          ...arrFromCount("scanned"),
          ...arrFromCount("updated"),
          ...arrFromCount("failed"),
          ...arrFromCount("fresh"),
          ...arrFromCount("stale"),
        ]
      : [];
  console.log("States: %o", states);
  const levels =
    form.log.value === "yes"
      ? [
          ...arrFromCount("error"),
          ...arrFromCount("warning"),
          ...arrFromCount("info"),
          ...arrFromCount("debug"),
        ]
      : [];
  console.log("Levels: %o", levels);
  const output = WATCHTOWER.tplprev(input, states, levels);
  console.log("Output: \n%o", output);
  if (output.startsWith("Error: ")) {
    document.querySelector(
      "#result"
    ).innerHTML = `<b>Error</b>: ${output.substring(7)}`;
  } else if (output.length) {
    document.querySelector("#result").innerText = output;
  } else {
    document.querySelector("#result").innerHTML =
      "<i>empty (would not be sent as a notification)</i>";
  }
};

const formSubmitted = (e) => {
  //e.preventDefault();
  //updatePreview();
};

let debounce;
const inputUpdated = () => {
  if (debounce) clearTimeout(debounce);
  debounce = setTimeout(() => updatePreview(), 400);
};

const formChanged = (e) => {
  console.log("form changed: %o", e);
  const targetToggle = e.target.dataset["toggle"];
  if (targetToggle) {
    e.target.form[targetToggle].value = e.target.checked ? "yes" : "no";
  }
  updatePreview();
};

const go = new Go();
WebAssembly.instantiateStreaming(
  fetch("../../assets/tplprev.wasm"),
  go.importObject
).then((result) => {
  go.run(result.instance);
  document.querySelector("#tplprev .loading").style.display = "none";
  wasmLoaded = true;
  updatePreview();
});

const loadQueryVals = () => {
  const form = document.querySelector("#tplprev");
  const params = new URLSearchParams(location.search);
  for (const [key, value] of params) {
    form[key].value = value;
    const toggleInput = form.querySelector(`[data-toggle="${key}"]`);
    if (toggleInput) {
      toggleInput.checked = value === "yes";
    }
  }
};

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => loadQueryVals());
} else {
  loadQueryVals();
}
