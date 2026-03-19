(function () {
  function updateSet(container, index) {
    const tabs = Array.from(container.querySelectorAll(".hextra-tabs-toggle"));
    tabs.forEach((tab, i) => {
      tab.dataset.state = i === index ? "selected" : "";
      if (i === index) {
        tab.setAttribute("aria-selected", "true");
        tab.tabIndex = 0;
      } else {
        tab.setAttribute("aria-selected", "false");
        tab.tabIndex = -1;
      }
    });
    const panelsContainer = container.parentElement.nextElementSibling;
    if (!panelsContainer) return;
    Array.from(panelsContainer.children).forEach((panel, i) => {
      panel.dataset.state = i === index ? "selected" : "";
      panel.setAttribute("aria-hidden", i === index ? "false" : "true");
      if (i === index) {
        panel.tabIndex = 0;
      } else {
        panel.removeAttribute("tabindex");
      }
    });
  }

  const syncSets = document.querySelectorAll("[data-tab-set]");

  syncSets.forEach((set) => {
    const key = encodeURIComponent(set.dataset.tabSet);
    const saved = localStorage.getItem("hextra-tab-" + key);
    if (saved !== null) {
      updateSet(set, parseInt(saved, 10));
    }
  });

  document.querySelectorAll(".hextra-tabs-toggle").forEach((button) => {
    button.addEventListener("click", function (e) {
      const targetButton = e.currentTarget;
      const container = targetButton.parentElement;
      const index = Array.from(
        container.querySelectorAll(".hextra-tabs-toggle"),
      ).indexOf(targetButton);

      if (container.dataset.tabSet) {
        // Sync behavior: update all tab sets with the same name.
        const tabSetValue = container.dataset.tabSet;
        const key = encodeURIComponent(tabSetValue);
        document
          .querySelectorAll('[data-tab-set="' + tabSetValue + '"]')
          .forEach((set) => updateSet(set, index));
        localStorage.setItem("hextra-tab-" + key, index.toString());
      } else {
        // Non-sync behavior: update only this specific tab set.
        updateSet(container, index);
      }
    });

    // Keyboard navigation for tabs
    button.addEventListener("keydown", function (e) {
      const container = button.parentElement;
      const tabs = Array.from(
        container.querySelectorAll(".hextra-tabs-toggle"),
      );
      const currentIndex = tabs.indexOf(button);
      let newIndex;

      switch (e.key) {
        case "ArrowRight":
        case "ArrowDown":
          e.preventDefault();
          newIndex = (currentIndex + 1) % tabs.length;
          break;
        case "ArrowLeft":
        case "ArrowUp":
          e.preventDefault();
          newIndex = (currentIndex - 1 + tabs.length) % tabs.length;
          break;
        case "Home":
          e.preventDefault();
          newIndex = 0;
          break;
        case "End":
          e.preventDefault();
          newIndex = tabs.length - 1;
          break;
        default:
          return;
      }

      if (container.dataset.tabSet) {
        const tabSetValue = container.dataset.tabSet;
        const key = encodeURIComponent(tabSetValue);
        document
          .querySelectorAll('[data-tab-set="' + tabSetValue + '"]')
          .forEach((set) => updateSet(set, newIndex));
        localStorage.setItem("hextra-tab-" + key, newIndex.toString());
      } else {
        updateSet(container, newIndex);
      }
      tabs[newIndex].focus();
    });
  });
})();
