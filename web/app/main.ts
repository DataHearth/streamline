import { mount } from "svelte";
import App from "./App.svelte";

const target = document.getElementById("app");
if (!target) {
	throw new Error("missing #app mount element in index.html");
}

mount(App, { target });
