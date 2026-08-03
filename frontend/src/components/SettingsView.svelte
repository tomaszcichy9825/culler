<script lang="ts">
  // The settings screen: the same window frame as every other, with the nav,
  // the page, and the aside inside it.
  //
  // It is a full-pane view rather than a dialog — the design draws it at the
  // window's own size — and it is modal only in the sense that it takes the
  // keyboard while it is up. Esc gives it back. Nothing here writes anything
  // on its own: the draft is edited freely and ⌘S is the only thing that
  // touches the file.

  import { onMount } from "svelte";
  import { PAGES, settings } from "../lib/settings.svelte";
  import type { PageId } from "../lib/settings.svelte";
  import { appearance } from "../lib/appearance.svelte";
  import AdvancedPage from "./settings/AdvancedPage.svelte";
  import AppearancePage from "./settings/AppearancePage.svelte";
  import CataloguePage from "./settings/CataloguePage.svelte";
  import CullingPage from "./settings/CullingPage.svelte";
  import FilesPage from "./settings/FilesPage.svelte";
  import GeneralPage from "./settings/GeneralPage.svelte";
  import KeymapPage from "./settings/KeymapPage.svelte";

  let root = $state<HTMLElement | null>(null);
  let filterBox = $state<HTMLInputElement | null>(null);
  /** Set when Esc arrives on a draft that has not been written. */
  let confirmDiscard = $state(false);

  let chip = $derived(PAGES.find((p) => p.id === settings.page)?.chip ?? "SETTINGS");
  let changed = $derived(settings.changedActions.length);

  onMount(() => {
    appearance.start();
    void settings.load();
    root?.focus();
  });

  /** A fresh edit takes the discard warning back down. */
  $effect(() => {
    if (settings.dirty) confirmDiscard = false;
  });

  function close() {
    if (settings.dirty && !confirmDiscard) {
      confirmDiscard = true;
      return;
    }
    confirmDiscard = false;
    settings.close();
  }

  function show(page: PageId) {
    settings.show(page);
    root?.focus();
  }

  /**
   * The screen's keys are taken in the capture phase so the app's own keymap
   * never sees them: k is a letter in a text field here, not a verdict. Keys
   * the screen does not claim are left alone, and a press that lands outside
   * the pane while it is up is stopped rather than reaching the grid behind.
   */
  function onKeydown(e: KeyboardEvent) {
    if (!settings.open) return;

    const inside = root !== null && e.target instanceof Node && root.contains(e.target);
    if (!inside) {
      e.stopPropagation();
      root?.focus();
    }

    // The recorder owns every key while it is open, Esc and Enter included.
    if (settings.recording !== null) return;

    const mod = e.metaKey || e.ctrlKey;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    if (mod && e.key.toLowerCase() === "s") {
      e.preventDefault();
      e.stopPropagation();
      void settings.save();
      return;
    }
    if (mod && e.key.toLowerCase() === "f") {
      e.preventDefault();
      e.stopPropagation();
      filterBox?.focus();
      filterBox?.select();
      return;
    }
    if (mod && e.key === ",") {
      e.preventDefault();
      e.stopPropagation();
      close();
    }
  }

  function onNavKeydown(e: KeyboardEvent) {
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
    e.preventDefault();
    e.stopPropagation();
    const at = PAGES.findIndex((p) => p.id === settings.page);
    const next = PAGES[Math.max(0, Math.min(at + (e.key === "ArrowDown" ? 1 : -1), PAGES.length - 1))];
    settings.show(next.id);
    queueMicrotask(() => document.querySelector<HTMLElement>(`[data-page="${next.id}"]`)?.focus());
  }
</script>

<svelte:window onkeydowncapture={onKeydown} />

<div
  class="settings"
  bind:this={root}
  data-keys="local"
  tabindex="-1"
  role="dialog"
  aria-modal="true"
  aria-label="Settings"
>
  <header class="titlebar">
    <span class="title">Settings</span>

    <div class="filter">
      <input
        bind:this={filterBox}
        bind:value={settings.filter}
        placeholder="filter settings"
        aria-label="Filter settings"
        spellcheck="false"
      />
      <span class="hint">⌘F</span>
    </div>

    <div class="file">
      <span class="path" title={settings.path}>{settings.path === "" ? "reading…" : settings.path}</span>
      <span class="pill dead" title="No backend call opens the file in an editor yet">edit file ⌘E</span>
      {#if settings.dirty}
        <button type="button" class="pill write" onclick={() => void settings.save()} disabled={settings.saving}>
          {settings.saving ? "writing…" : "write ⌘S"}
        </button>
      {/if}
    </div>
  </header>

  {#if settings.error !== ""}
    <div class="banner" role="alert">{settings.error}</div>
  {/if}

  <div class="body">
    <nav class="nav" aria-label="Settings pages">
      {#each PAGES as page (page.id)}
        <button
          type="button"
          class="navrow"
          class:active={settings.page === page.id}
          data-page={page.id}
          aria-current={settings.page === page.id}
          tabindex={settings.page === page.id ? 0 : -1}
          onkeydown={onNavKeydown}
          onclick={() => show(page.id)}
        >
          <span class="name">{page.label}</span>
          {#if page.id === "keymap" && changed > 0}<span class="badge">{changed}</span>{/if}
        </button>
      {/each}
    </nav>

    {#if settings.draft === null}
      <div class="loading">{settings.loading ? "reading the settings file…" : "no settings loaded"}</div>
    {:else if settings.page === "general"}
      <GeneralPage />
    {:else if settings.page === "keymap"}
      <KeymapPage />
    {:else if settings.page === "culling"}
      <CullingPage />
    {:else if settings.page === "files"}
      <FilesPage />
    {:else if settings.page === "catalogue"}
      <CataloguePage />
    {:else if settings.page === "appearance"}
      <AppearancePage />
    {:else}
      <AdvancedPage />
    {/if}
  </div>

  <footer class="statusbar">
    <span class="chip">{chip}</span>
    <span class="item"><span class="key">⌘S</span> write</span>
    <span class="item"><span class="key">⌫</span> reset a binding</span>
    <span class="item"><span class="key">esc</span> {confirmDiscard ? "again to discard" : "close"}</span>
    <span class="spacer"></span>
    {#if confirmDiscard}
      <button type="button" class="revert" onclick={() => settings.revert()}>discard changes</button>
    {/if}
    <span class="state" class:dirty={settings.dirty} class:written={settings.written && !settings.dirty}>
      {#if settings.dirty}unsaved — nothing is written until ⌘S{:else if settings.written}written to the file{:else}
        in step with the file
      {/if}
    </span>
  </footer>
</div>

<style>
  .settings {
    position: fixed;
    inset: 0;
    z-index: 40;
    display: flex;
    flex-direction: column;
    background: var(--bg-window);
    outline: none;
  }

  .titlebar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 14px;
    height: 40px;
    /* The window's traffic lights sit in the same place on every screen. */
    padding: 0 14px 0 78px;
    background: var(--bg-chrome);
    border-bottom: 1px solid var(--border);
  }

  .title {
    font-size: 12px;
    font-weight: 600;
    color: var(--text);
    white-space: nowrap;
  }

  .filter {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 26px;
    padding: 0 10px;
    border-radius: 6px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
  }

  .filter:focus-within {
    border-color: var(--border-focus);
  }

  .filter input {
    flex: 1;
    min-width: 0;
    border: none;
    background: none;
    outline: none;
    font-family: inherit;
    font-size: 11.5px;
    color: var(--text);
  }

  .filter input::placeholder {
    color: var(--text-dim);
  }

  .filter .hint {
    flex: 0 0 auto;
    font-size: 11px;
    color: var(--text-muted);
  }

  .file {
    display: flex;
    align-items: center;
    gap: 10px;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .path {
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    /* A path is worth more from its end than its start, so it is the head that
       is dropped. plaintext keeps the slashes where they belong: rtl alone
       moves the leading one to the far side. */
    direction: rtl;
    unicode-bidi: plaintext;
    text-align: right;
  }

  .pill {
    padding: 3px 9px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    white-space: nowrap;
  }

  .pill.dead {
    opacity: 0.55;
  }

  .pill.write {
    background: var(--keep);
    border-color: var(--keep);
    color: var(--on-accent);
    font-weight: 700;
    cursor: pointer;
  }

  .pill.write:disabled {
    opacity: 0.6;
    cursor: progress;
  }

  .banner {
    flex: 0 0 auto;
    padding: 6px 14px;
    background: var(--cut-wash-14);
    border-bottom: 1px solid var(--cut);
    color: var(--cut);
    font-size: 11px;
    overflow-wrap: anywhere;
  }

  .body {
    flex: 1;
    min-height: 0;
    display: flex;
  }

  .nav {
    flex: 0 0 190px;
    display: flex;
    flex-direction: column;
    padding: 10px 0;
    background: var(--bg-pane);
    border-right: 1px solid var(--border);
    overflow-y: auto;
  }

  .navrow {
    display: flex;
    align-items: center;
    gap: 9px;
    height: 28px;
    padding: 0 12px;
    border: none;
    border-left: 2px solid transparent;
    background: none;
    font: inherit;
    text-align: left;
    color: var(--text-muted);
    cursor: pointer;
  }

  .navrow:hover {
    color: var(--text);
  }

  .navrow.active {
    background: var(--accent-wash-16);
    border-left-color: var(--accent);
    color: var(--text-hi);
  }

  .navrow:focus-visible {
    outline: none;
    box-shadow: var(--focus-inset-2);
  }

  .navrow .name {
    flex: 1;
    min-width: 0;
    font-size: 11.5px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .badge {
    flex: 0 0 auto;
    font-size: 10px;
    color: var(--gold);
  }

  .loading {
    flex: 1;
    display: grid;
    place-items: center;
    color: var(--text-dim);
    font-size: 11.5px;
  }

  .statusbar {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    gap: 14px;
    height: 30px;
    padding: 0 12px;
    background: var(--bg-chrome);
    border-top: 1px solid var(--border);
    font-size: 11px;
    color: var(--text-dim);
  }

  .chip {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 2px 7px;
    border-radius: 3px;
    background: var(--accent-wash-16);
    color: var(--accent);
    white-space: nowrap;
  }

  .item {
    white-space: nowrap;
  }

  .key {
    color: var(--text-muted);
  }

  .spacer {
    flex: 1;
  }

  .revert {
    padding: 2px 8px;
    border-radius: 4px;
    background: var(--bg-field);
    border: 1px solid var(--border-strong);
    font: inherit;
    font-size: 11px;
    color: var(--text-muted);
    cursor: pointer;
  }

  .state {
    color: var(--text-muted);
    white-space: nowrap;
  }

  .state.dirty {
    color: var(--gold);
  }

  .state.written {
    color: var(--keep);
  }
</style>
