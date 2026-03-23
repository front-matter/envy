window.envyEditor = function envyEditor(config = {}) {
  const initialValue =
    typeof config.initialValue === "string" ? config.initialValue : "";
  const sendingFlashDuration =
    typeof config.sendingFlashDuration === "number"
      ? config.sendingFlashDuration
      : 500;
  const successFlashDuration =
    typeof config.successFlashDuration === "number"
      ? config.successFlashDuration
      : 1000;
  const errorFlashDuration =
    typeof config.errorFlashDuration === "number"
      ? config.errorFlashDuration
      : 1200;
  const inputRef =
    typeof config.inputRef === "string" && config.inputRef
      ? config.inputRef
      : "input";
  const primaryField =
    typeof config.field === "string" ? config.field.trim() : "";
  const deleteOnEmptySubmit = config.deleteOnEmptySubmit === true;
  const deleteEntityLabel =
    typeof config.deleteEntityLabel === "string" && config.deleteEntityLabel
      ? config.deleteEntityLabel
      : "item";
  const deleteApiBase =
    typeof config.deleteApiBase === "string" && config.deleteApiBase
      ? config.deleteApiBase.replace(/\/$/, "")
      : "/api/profiles";
  const deleteUseAlpineModal = config.deleteUseAlpineModal === true;
  const deleteSection =
    typeof config.deleteSection === "string" && config.deleteSection
      ? config.deleteSection.trim().replace(/^\/+|\/+$/g, "")
      : "profiles";

  return {
    isEditing: false,
    displayValue: initialValue,
    draftValue: initialValue,
    currentField: primaryField,
    isConfirmingDelete: false,
    isDeleteModalOpen: false,
    flashKind: "",
    flashTimer: null,

    init() {
      this.$el.addEventListener("htmx:beforeRequest", () => {
        this.startSendFeedback();
      });
      this.$el.addEventListener("htmx:afterRequest", (event) => {
        this.handleAfterRequest(event);
      });
    },

    enterEdit() {
      this.isEditing = true;
      this.draftValue = this.displayValue;
      this.$nextTick(() => {
        const input = this.$refs[inputRef];
        if (input) {
          input.focus();
          if (typeof input.select === "function") {
            input.select();
          }
        }
      });
    },

    cancelEdit() {
      this.isEditing = false;
      this.draftValue = this.displayValue;
      this.currentField = primaryField;
    },

    handleBlur() {
      // Opening the native confirm dialog moves focus away from the textarea.
      // Ignore that blur so delete submits can proceed.
      if (this.isConfirmingDelete || this.isDeleteModalOpen) {
        return;
      }
      this.cancelEdit();
    },

    openDeleteModal() {
      this.isDeleteModalOpen = true;
    },

    closeDeleteModal() {
      this.isDeleteModalOpen = false;
      this.$nextTick(() => {
        const input = this.$refs[inputRef];
        if (input) {
          input.focus();
        }
      });
    },

    confirm() {
      this.isDeleteModalOpen = false;
      this.submitDelete();
    },

    submitEdit() {
      const draftIsEmpty = !this.draftValue.trim();
      if (deleteOnEmptySubmit && draftIsEmpty) {
        if (deleteUseAlpineModal) {
          this.openDeleteModal();
          return;
        }

        const targetName = (this.displayValue || "").trim();
        const confirmMessage = `Delete this ${deleteEntityLabel}?\n\nThis will permanently remove "${targetName}". This action cannot be undone.`;
        this.isConfirmingDelete = true;
        const confirmed = window.confirm(confirmMessage);
        this.isConfirmingDelete = false;
        if (!confirmed) {
          return;
        }
        this.submitDelete();
        return;
      } else {
        this.currentField = primaryField;
      }

      if (this.$refs.form) {
        this.$refs.form.requestSubmit();
      }
    },

    submitDelete() {
      const pageInput = this.$el.querySelector('input[name="page"]');
      let pagePath = pageInput ? pageInput.value.trim() : "";
      if (
        !pagePath &&
        window.location &&
        typeof window.location.pathname === "string"
      ) {
        pagePath = window.location.pathname;
      }
      const sectionPattern = deleteSection.replace(
        /[.*+?^${}()|[\]\\]/g,
        "\\$&",
      );
      const slugMatch = pagePath.match(
        new RegExp(`/${sectionPattern}/([^/]+)/?$`),
      );
      const profileSlug = slugMatch ? slugMatch[1].trim() : "";

      if (!profileSlug) {
        this.flash("error", errorFlashDuration);
        return;
      }

      this.startSendFeedback();
      const deleteUrl = `${deleteApiBase}/${encodeURIComponent(profileSlug)}?page=${encodeURIComponent(pagePath)}`;

      fetch(deleteUrl, {
        method: "DELETE",
        headers: {
          "HX-Request": "true",
        },
      })
        .then((resp) => {
          if (!resp.ok) {
            resp
              .text()
              .then((msg) => console.error("Failed to delete entity:", msg));
            this.flash("error", errorFlashDuration);
            return;
          }

          const redirect = resp.headers.get("HX-Redirect");
          if (redirect) {
            window.location.href = redirect;
            return;
          }

          this.flash("success", successFlashDuration);
          this.isEditing = false;
        })
        .catch(() => {
          this.flash("error", errorFlashDuration);
        });
    },

    clearFlash() {
      if (this.flashTimer) {
        window.clearTimeout(this.flashTimer);
        this.flashTimer = null;
      }
      this.flashKind = "";
    },

    flash(kind, duration = 450) {
      this.clearFlash();
      this.flashKind = kind;
      this.flashTimer = window.setTimeout(() => {
        this.flashKind = "";
        this.flashTimer = null;
      }, duration);
    },

    startSendFeedback() {
      this.flash("sending", sendingFlashDuration);
    },

    handleAfterRequest(event) {
      const successful = !!(event && event.detail && event.detail.successful);
      if (successful) {
        if (this.currentField !== "delete") {
          this.displayValue = this.draftValue;
        }
        this.isEditing = false;
        this.currentField = primaryField;
        this.flash("success", successFlashDuration);
        return;
      }

      this.currentField = primaryField;
      this.flash("error", errorFlashDuration);
    },

    flashClasses() {
      if (this.flashKind === "sending") {
        return "hx:ring-2 hx:ring-blue-300 hx:dark:ring-blue-500";
      }
      if (this.flashKind === "success") {
        return "hx:ring-2 hx:ring-green-300 hx:dark:ring-green-500";
      }
      if (this.flashKind === "error") {
        return "hx:ring-2 hx:ring-red-300 hx:dark:ring-red-500";
      }
      return "";
    },
  };
};

// envyProfileAdder is an Alpine.js component for the tag-input on the profiles
// overview page that allows creating new profiles via POST /api/profiles.
window.envyProfileAdder = function envyProfileAdder() {
  return {
    isEditing: false,
    draftValue: "",
    flashKind: "",
    flashTimer: null,

    enterEdit() {
      this.isEditing = true;
      this.$nextTick(() => {
        const input = this.$refs.profileInput;
        if (input) {
          input.focus();
          if (typeof input.select === "function") {
            input.select();
          }
        }
      });
    },

    cancelEdit() {
      this.isEditing = false;
      this.draftValue = "";
    },

    addProfile(name) {
      const trimmed = name.replace(/,+$/, "").trim();
      if (!trimmed) return;

      this.draftValue = "";

      this.flash("sending", 500);

      const formData = new URLSearchParams();
      formData.set("field", "create");
      formData.set("value", trimmed);

      fetch("/api/profiles", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: formData.toString(),
      })
        .then((resp) => {
          if (resp.ok) {
            const redirect = resp.headers.get("HX-Redirect");
            if (redirect) {
              this.redirectWhenReady(redirect);
              return;
            }
            this.flash("success", 1000);
          } else {
            resp
              .text()
              .then((msg) => console.error("Failed to add profile:", msg));
            this.flash("error", 1200);
          }
        })
        .catch((err) => {
          console.error("Error adding profile:", err);
          this.flash("error", 1200);
        });
    },

    redirectWhenReady(redirect, attempt = 0) {
      const maxAttempts = 20;
      const retryDelayMs = 150;

      fetch(redirect, {
        method: "GET",
        headers: {
          "X-Requested-With": "envy-profile-adder",
        },
      })
        .then((resp) => {
          if (resp.status !== 404 || attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        })
        .catch(() => {
          if (attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        });
    },

    removeLastTag() {
      // Reserved for future multi-tag management.
    },

    handleKeydown(e) {
      if ((e.key === "Enter" && !e.shiftKey) || e.key === ",") {
        e.preventDefault();
        this.addProfile(this.draftValue);
      }
      if (e.key === "Escape") {
        e.preventDefault();
        this.cancelEdit();
      }
      if (e.key === "Backspace" && !this.draftValue.trim()) {
        this.removeLastTag();
      }
    },

    flash(kind, duration) {
      if (this.flashTimer) clearTimeout(this.flashTimer);
      this.flashKind = kind;
      this.flashTimer = setTimeout(() => {
        this.flashKind = "";
        this.flashTimer = null;
      }, duration);
    },

    flashClasses() {
      if (this.flashKind === "sending")
        return "hx:ring-2 hx:ring-blue-300 hx:dark:ring-blue-500";
      if (this.flashKind === "success")
        return "hx:ring-2 hx:ring-green-300 hx:dark:ring-green-500";
      if (this.flashKind === "error")
        return "hx:ring-2 hx:ring-red-300 hx:dark:ring-red-500";
      return "";
    },

    cardStateClasses() {
      if (this.isEditing) {
        return "hx:border-blue-300 hx:dark:border-blue-700 hx:shadow-md";
      }
      return "";
    },
  };
};

// envySetAdder is an Alpine.js component for the tag-input on the sets
// overview page that allows creating new sets via POST /api/sets.
window.envySetAdder = function envySetAdder() {
  return {
    isEditing: false,
    draftValue: "",
    flashKind: "",
    flashTimer: null,

    enterEdit() {
      this.isEditing = true;
      this.$nextTick(() => {
        const input = this.$refs.setInput;
        if (input) {
          input.focus();
          if (typeof input.select === "function") {
            input.select();
          }
        }
      });
    },

    cancelEdit() {
      this.isEditing = false;
      this.draftValue = "";
    },

    addSet(name) {
      const trimmed = name.replace(/,+$/, "").trim();
      if (!trimmed) return;

      this.draftValue = "";

      this.flash("sending", 500);

      const formData = new URLSearchParams();
      formData.set("field", "create");
      formData.set("value", trimmed);

      fetch("/api/sets", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: formData.toString(),
      })
        .then((resp) => {
          if (resp.ok) {
            const redirect = resp.headers.get("HX-Redirect");
            if (redirect) {
              this.redirectWhenReady(redirect);
              return;
            }
            this.flash("success", 1000);
          } else {
            resp.text().then((msg) => console.error("Failed to add set:", msg));
            this.flash("error", 1200);
          }
        })
        .catch((err) => {
          console.error("Error adding set:", err);
          this.flash("error", 1200);
        });
    },

    redirectWhenReady(redirect, attempt = 0) {
      const maxAttempts = 20;
      const retryDelayMs = 150;

      fetch(redirect, {
        method: "GET",
        headers: {
          "X-Requested-With": "envy-set-adder",
        },
      })
        .then((resp) => {
          if (resp.status !== 404 || attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        })
        .catch(() => {
          if (attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        });
    },

    handleKeydown(e) {
      if ((e.key === "Enter" && !e.shiftKey) || e.key === ",") {
        e.preventDefault();
        this.addSet(this.draftValue);
      }
      if (e.key === "Escape") {
        e.preventDefault();
        this.cancelEdit();
      }
    },

    flash(kind, duration) {
      if (this.flashTimer) clearTimeout(this.flashTimer);
      this.flashKind = kind;
      this.flashTimer = setTimeout(() => {
        this.flashKind = "";
        this.flashTimer = null;
      }, duration);
    },

    flashClasses() {
      if (this.flashKind === "sending")
        return "hx:ring-2 hx:ring-blue-300 hx:dark:ring-blue-500";
      if (this.flashKind === "success")
        return "hx:ring-2 hx:ring-green-300 hx:dark:ring-green-500";
      if (this.flashKind === "error")
        return "hx:ring-2 hx:ring-red-300 hx:dark:ring-red-500";
      return "";
    },

    cardStateClasses() {
      if (this.isEditing) {
        return "hx:border-blue-300 hx:dark:border-blue-700 hx:shadow-md";
      }
      return "";
    },
  };
};

// envyServiceAdder is an Alpine.js component for the tag-input on the services
// overview page that allows creating new services via POST /api/services.
window.envyServiceAdder = function envyServiceAdder() {
  return {
    isEditing: false,
    draftValue: "",
    flashKind: "",
    flashTimer: null,

    enterEdit() {
      this.isEditing = true;
      this.$nextTick(() => {
        const input = this.$refs.serviceInput;
        if (input) {
          input.focus();
          if (typeof input.select === "function") {
            input.select();
          }
        }
      });
    },

    cancelEdit() {
      this.isEditing = false;
      this.draftValue = "";
    },

    addService(name) {
      const trimmed = name.replace(/,+$/, "").trim();
      if (!trimmed) return;

      this.draftValue = "";

      this.flash("sending", 500);

      const formData = new URLSearchParams();
      formData.set("field", "create");
      formData.set("value", trimmed);

      fetch("/api/services", {
        method: "POST",
        headers: {
          "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
        },
        body: formData.toString(),
      })
        .then((resp) => {
          if (resp.ok) {
            const redirect = resp.headers.get("HX-Redirect");
            if (redirect) {
              this.redirectWhenReady(redirect);
              return;
            }
            this.flash("success", 1000);
          } else {
            resp
              .text()
              .then((msg) => console.error("Failed to add service:", msg));
            this.flash("error", 1200);
          }
        })
        .catch((err) => {
          console.error("Error adding service:", err);
          this.flash("error", 1200);
        });
    },

    redirectWhenReady(redirect, attempt = 0) {
      const maxAttempts = 20;
      const retryDelayMs = 150;

      fetch(redirect, {
        method: "GET",
        headers: {
          "X-Requested-With": "envy-service-adder",
        },
      })
        .then((resp) => {
          if (resp.status !== 404 || attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        })
        .catch(() => {
          if (attempt >= maxAttempts) {
            window.location.href = redirect;
            return;
          }
          setTimeout(() => {
            this.redirectWhenReady(redirect, attempt + 1);
          }, retryDelayMs);
        });
    },

    handleKeydown(e) {
      if ((e.key === "Enter" && !e.shiftKey) || e.key === ",") {
        e.preventDefault();
        this.addService(this.draftValue);
      }
      if (e.key === "Escape") {
        e.preventDefault();
        this.cancelEdit();
      }
    },

    flash(kind, duration) {
      if (this.flashTimer) clearTimeout(this.flashTimer);
      this.flashKind = kind;
      this.flashTimer = setTimeout(() => {
        this.flashKind = "";
        this.flashTimer = null;
      }, duration);
    },

    flashClasses() {
      if (this.flashKind === "sending")
        return "hx:ring-2 hx:ring-blue-300 hx:dark:ring-blue-500";
      if (this.flashKind === "success")
        return "hx:ring-2 hx:ring-green-300 hx:dark:ring-green-500";
      if (this.flashKind === "error")
        return "hx:ring-2 hx:ring-red-300 hx:dark:ring-red-500";
      return "";
    },

    cardStateClasses() {
      if (this.isEditing) {
        return "hx:border-blue-300 hx:dark:border-blue-700 hx:shadow-md";
      }
      return "";
    },
  };
};
