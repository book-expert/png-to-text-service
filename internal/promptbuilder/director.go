package promptbuilder

import (
	"fmt"
	"strings"
)

// DirectorConfig holds the user-defined settings for the narration.
// These settings MUST be consistent across all pages of a document to ensure uniform audio.
type DirectorConfig struct {
	// StyleProfile: e.g., "Podcast", "Audiobook", "Technical", "News"
	StyleProfile string
	// Voice: The specific voice name (e.g., "Fenrir", "Puck").
	Voice string
	// CustomInstructions: Specific user requests (e.g., "Skip the table of contents").
	CustomInstructions string
	// Exclusions: List of things to ignore (headers, footers, citations).
	Exclusions []string
}

// BuildSystemInstruction constructs the master prompt for the Gemini Vision model.
// CRITICAL: This prompt forces the LLM to adopt a SPECIFIC, CONSISTENT persona defined by the config.
// The LLM is NOT allowed to invent the scene; it must adopt the one provided here.
func BuildSystemInstruction(cfg DirectorConfig) string {
	var sb strings.Builder

	// 1. Role Definition & Consistency Mandate
	sb.WriteString("You are an expert Audio Director and Narrator. Your task is to convert a document image into a high-quality Audio Script for Text-to-Speech generation.\n")
	sb.WriteString("CRITICAL: You must maintain the **exact** Audio Profile, Scene, and Director's Notes defined below. Do not hallucinate new scenes or change the persona. Consistency across pages is mandatory.\n\n")

	// 2. Tone/Style Configuration (The Fixed \"Director's Vision\")
	profile := resolveStyleProfile(cfg.StyleProfile, cfg.Voice)
	
sb.WriteString("### TARGET AUDIO PROFILE (DO NOT CHANGE):\n")
sb.WriteString(fmt.Sprintf("- **Persona Name:** %s\n", profile.Name))
sb.WriteString(fmt.Sprintf("- **Role:** %s\n", profile.Role))
sb.WriteString(fmt.Sprintf("- **Scene Title:** %s\n", profile.SceneTitle))
sb.WriteString(fmt.Sprintf("- **Vibe:** %s\n", profile.SceneDescription))
sb.WriteString("\n")

	// 3. Core Tasks (Vision & Cleanup)
	sb.WriteString("### YOUR TASKS:\n")
sb.WriteString("1. **Analyze & Extract:** Read the image. Extract all text. Expand acronyms (e.g., 'AI' -> 'Artificial Intelligence'). Fix line breaks.\n")
sb.WriteString("2. **Visual Narration:** If you see charts, graphs, or photos, write a brief, natural narration of what they convey (e.g., 'The chart shows a sharp increase in sales...'). Do NOT say 'Image of...'.\n")
sb.WriteString("3. **Cleanup:** Remove non-narratable elements: Headers, Footers, Page Numbers, URLs, Citations.\n")
sb.WriteString("4. **Format:** Output the result strictly using the template below.\n\n")

	// 4. User Constraints
	sb.WriteString("### USER CONSTRAINTS:\n")
	if len(cfg.Exclusions) > 0 {
		sb.WriteString("STRICTLY EXCLUDE:\n")
		for _, ex := range cfg.Exclusions {
			sb.WriteString(fmt.Sprintf("- %s\n", ex))
		}
	}
	if cfg.CustomInstructions != "" {
		sb.WriteString(fmt.Sprintf("Additional Instructions: %s\n", cfg.CustomInstructions))
	}
	sb.WriteString("\n")

	// 5. Output Template
	// We provide the EXACT strings for the header sections so the LLM just copies them.
	sb.WriteString("### OUTPUT TEMPLATE (STRICT MARKDOWN):\n")
	sb.WriteString("You must output ONLY this structure:\n\n")
	sb.WriteString(fmt.Sprintf("# AUDIO PROFILE: %s\n", profile.Name))
	sb.WriteString(fmt.Sprintf("## \"%s\"\n\n", profile.Role))
	
	// Dynamic Scene Generation Instruction
	sb.WriteString("## THE SCENE: <Create a brief Title for the scene based on the page content>\n")
	sb.WriteString("<Describe the scene vividly. Keep it consistent with the Persona, but adapt to the content (e.g., if the text is intense, the scene should reflect that).>\n\n")
	
	sb.WriteString("### DIRECTOR'S NOTES\n")
	sb.WriteString("Style: <Define the specific vocal style for this page (e.g., 'Urgent', 'Calm', 'Pedantic'). Must align with the base Persona.>\n")
	sb.WriteString("Pace: <Define the pacing (e.g., 'Fast and punchy', 'Slow and deliberate'). Match the content density.>\n")
	sb.WriteString(fmt.Sprintf("Accent: %s\n\n", profile.Accent)) // Keep Accent fixed
	
	sb.WriteString("### SAMPLE CONTEXT\n")
	sb.WriteString("<Write a 1-sentence context setting for the actor. E.g., 'You are in the middle of explaining a complex theory...'>\n\n")
	
	sb.WriteString("#### TRANSCRIPT\n")
	sb.WriteString("<Your cleaned, expanded, and narrated text goes here>\n")

	return sb.String()
}

// internal struct for style logic
type styleDef struct {
	Name             string
	Role             string
	SceneTitle       string
	SceneDescription string
	Style            string
	Pace             string
	Accent           string
	Context          string
}

func resolveStyleProfile(profileName, voiceName string) styleDef {
	// Default to "Standard"
	if voiceName == "" {
		voiceName = "Narrator"
	}

	def := styleDef{
		Name:             voiceName,
		Role:             "Professional Narrator",
		SceneTitle:       "The Recording Booth",
		SceneDescription: "A quiet, professional sound booth with a warm microphone.",
		Style:            "Clear, engaging, and professional. Use a 'Vocal Smile'.",
		Pace:             "Steady and articulate.",
		Accent:           "Neutral American",
		Context:          "Reading a document for a general audience.",
	}

	switch strings.ToLower(profileName) {
	case "academic", "technical":
		def.Role = "Professor"
		def.SceneTitle = "The Lecture Hall"
		def.SceneDescription = "Standing at a podium in a quiet university hall, addressing attentive students."
		def.Style = "Authoritative, precise, and educational. Use pauses for emphasis on key terms."
		def.Pace = "Deliberate and clear."
		def.Context = "Delivering a lecture on the subject matter."
	case "storyteller", "fiction":
		def.Role = "Storyteller"
		def.SceneTitle = "By the Fireplace"
		def.SceneDescription = "Sitting in a comfortable armchair by a crackling fire, reading to a captivated audience."
		def.Style = "Expressive, dynamic, and emotional. Use vocal variation to act out the text."
		def.Pace = "Fluid, slowing down for dramatic effect."
		def.Context = "Immersing the listener in a narrative journey."
	case "news", "report":
		def.Role = "News Anchor"
		def.SceneTitle = "The News Desk"
		def.SceneDescription = "Bright studio lights, breaking news ticker in the background. High energy."
		def.Style = "Punchy, objective, and urgent. Clear articulation."
		def.Pace = "Brisk and energetic."
		def.Context = "Reporting on important events or data."
	case "podcast":
		def.Role = "Podcast Host"
		def.SceneTitle = "The Home Studio"
		def.SceneDescription = "Casual setup with a coffee mug. Intimate and conversational."
		def.Style = "Friendly, conversational, and relatable. 'Vocal smile'."
		def.Pace = "Natural and flowing."
		def.Context = "Discussing the document with a friend."
	}

	return def
}