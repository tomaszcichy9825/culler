<script lang="ts">
  import { app } from "../lib/state.svelte";
</script>

{#if app.toast}
  {#key app.toast.id}
    <div class="toast" class:error={app.toast.tone === "error"} role="status" title={app.toast.message}>
      {app.toast.message}
    </div>
  {/key}
{/if}

<style>
  .toast {
    position: fixed;
    z-index: 40;
    left: 50%;
    bottom: 54px;
    transform: translateX(-50%);
    /* A long error can carry a full path; it truncates and keeps the whole
       value in the title rather than growing across the window. */
    max-width: min(70vw, 520px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    padding: 8px 14px;
    border-radius: 8px;
    background: var(--bg-panel);
    border: 1px solid var(--border-strong);
    box-shadow: var(--shadow-toast);
    color: var(--text);
    font-size: 12.5px;
    pointer-events: none;
    animation: rise 0.16s ease-out;
  }

  .toast.error {
    border-color: var(--error-border);
    color: var(--error-text);
  }

  @keyframes rise {
    from {
      opacity: 0;
      transform: translate(-50%, 6px);
    }
    to {
      opacity: 1;
      transform: translate(-50%, 0);
    }
  }
</style>
