# 📖 Architecture Decision Records (ADRs)

This folder contains **Architecture Decision Records (ADRs)** for the project. ADRs document significant technical and architectural decisions made during the development process, providing context, trade-offs, and reasoning behind each choice.

## 📌 What is an ADR?

An **ADR (Architecture Decision Record)** is a document that captures a key architectural decision, along with its context and consequences. It serves as a historical reference for why specific decisions were made and helps maintain consistency as the project evolves.

## 📂 Folder Structure

Each ADR is stored as a markdown file in this folder. The naming convention follows:

```text
<ADR_NUMBER>-<SHORT_TITLE>.md
```

Example:

```text
001-initial-architecture.md
002-database-choice.md
003-api-design.md
```

Each number represents the order in which the ADR was created, ensuring a chronological history of decisions with a quick glance. Any new ADR should be added with the next available number.

As a general note, if an existing decision is revisited or changed, a new ADR should be created rather than modifying the existing one. This maintains a clear history of decisions and their evolution.

## 🛠 ADR Template

Each ADR follows a standardized structure that is available in the `0000_template.md` file. Please use this template when creating new ADRs to ensure consistency across the documentation. If you feel the need to adjust the template, think twice and discuss it with the team first. Still want to change it? Feel free to do so!

## 🎯 Why Use ADRs?

- 📌 **Traceability** – Documents the reasoning behind key decisions.
- 🔍 **Transparency** – Helps new team members understand past decisions.
- 🔄 **Consistency** – Ensures architectural coherence throughout the project.
- 🏗 **Future-Proofing** – Allows for revisiting decisions when requirements change.
