<script lang="ts">
  // The grid, loupe and keymap layer land on top of this shell. It exists so
  // the app builds and runs against the real backend services while the UI is
  // being written.
</script>

<main class="shell">
  <p>culler</p>
</main>

<style>
  .shell {
    display: grid;
    place-items: center;
    height: 100vh;
    font-family: system-ui, sans-serif;
    color: #e8e8ea;
  }
</style>
