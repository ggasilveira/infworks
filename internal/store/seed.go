package store

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"infworks/internal/domain"
)

// DemoUsers are the mock accounts standing in for the UFRGS identity service.
var DemoUsers = []struct {
	User     domain.User
	Password string
}{
	{domain.User{Username: "library", DisplayName: "Library Worker (demo)", Role: domain.RoleLibraryWorker}, "library"},
	{domain.User{Username: "coordinator", DisplayName: "Course Coordinator (demo)", Role: domain.RoleCoordinator}, "coordinator"},
	{domain.User{Username: "coordinator2", DisplayName: "Second Coordinator (demo)", Role: domain.RoleCoordinator}, "coordinator2"},
	{domain.User{Username: "researcher", DisplayName: "Marco (researcher, demo)", Role: domain.RoleResearcher}, "researcher"},
}

type seedWork struct {
	title    string
	year     int
	typ      domain.WorkType
	author   string
	advisor  string
	area     string
	keywords string
}

// All people, titles and links below are fictional demo data.
var seedPublished = []seedWork{
	{"Graph Search Heuristics for Campus Route Planning", 2021, domain.TypeTCC, "Lucas Fernandes", "Helena Duarte", "Artificial Intelligence", "graph search, heuristics, route planning"},
	{"Machine Learning Models for Early Dropout Prediction in Undergraduate Programs", 2023, domain.TypeDissertation, "Ana Paula Rocha", "Helena Duarte", "Artificial Intelligence", "machine learning, education, prediction"},
	{"Transformer-Based Summarization of Portuguese Legal Texts", 2024, domain.TypeThesis, "Rafael Teixeira", "Helena Duarte", "Artificial Intelligence", "natural language processing, machine learning, transformers"},
	{"Congestion Control in Low-Earth-Orbit Satellite Networks", 2022, domain.TypeThesis, "Mariana Costa", "Ricardo Lima Prates", "Computer Networks", "congestion control, satellite networks, tcp"},
	{"Evaluating QUIC Performance on Mobile Networks", 2023, domain.TypeTCC, "Gabriel Souza", "Ricardo Lima Prates", "Computer Networks", "quic, mobile networks, performance evaluation"},
	{"Programmable Data Planes for In-Network Telemetry", 2025, domain.TypeDissertation, "Camila Martins", "Ricardo Lima Prates", "Computer Networks", "p4, telemetry, software-defined networking"},
	{"Test Smells in Open-Source Python Projects", 2022, domain.TypeTCC, "Bruno Almeida", "Beatriz Ferraz", "Software Engineering", "software testing, test smells, python"},
	{"Requirements Traceability with Lightweight Models", 2021, domain.TypeDissertation, "Juliana Pereira", "Beatriz Ferraz", "Software Engineering", "requirements engineering, traceability, uml"},
	{"Large Language Models as Pair Programmers: An Empirical Study", 2025, domain.TypeTCC, "Pedro Henrique Lopes", "Beatriz Ferraz", "Software Engineering", "machine learning, software engineering, empirical study"},
	{"Microservice Decomposition from Monolithic Codebases", 2024, domain.TypeThesis, "Fernanda Ribeiro", "Beatriz Ferraz", "Software Engineering", "microservices, software architecture, refactoring"},
	{"Real-Time Global Illumination with Sparse Voxel Octrees", 2020, domain.TypeDissertation, "Thiago Nunes", "Jorge Albuquerque", "Computer Graphics", "rendering, global illumination, gpu"},
	{"Procedural Terrain Generation Using Noise Functions", 2022, domain.TypeTCC, "Isabela Cardoso", "Jorge Albuquerque", "Computer Graphics", "procedural generation, terrain, noise"},
	{"Neural Radiance Fields for Cultural Heritage Digitization", 2024, domain.TypeDissertation, "Vinícius Barbosa", "Jorge Albuquerque", "Computer Graphics", "neural rendering, machine learning, 3d reconstruction"},
	{"Query Optimization for Temporal Graph Databases", 2023, domain.TypeThesis, "Larissa Gomes", "Sofia Menezes", "Databases", "query optimization, graph databases, temporal data"},
	{"Indexing Strategies for Geospatial Time Series", 2021, domain.TypeTCC, "Eduardo Moura", "Sofia Menezes", "Databases", "indexing, time series, geospatial data"},
	{"Privacy-Preserving Record Linkage in Public Health Data", 2025, domain.TypeDissertation, "Natália Freitas", "Sofia Menezes", "Databases", "privacy, record linkage, data integration"},
	{"Parameterized Complexity of Graph Coloring Variants", 2022, domain.TypeThesis, "Diego Araújo", "Paulo Vasconcelos", "Theory of Computation", "graph theory, parameterized complexity, coloring"},
	{"Formal Verification of Smart Contracts with Model Checking", 2024, domain.TypeTCC, "Letícia Monteiro", "Paulo Vasconcelos", "Theory of Computation", "formal verification, model checking, smart contracts"},
	{"Energy-Aware Scheduling for Embedded Multicore Systems", 2023, domain.TypeDissertation, "Matheus Correia", "Ricardo Lima Prates", "Embedded Systems", "scheduling, energy efficiency, embedded systems"},
	{"TinyML Keyword Spotting on Microcontrollers", 2025, domain.TypeTCC, "Carolina Dias", "Helena Duarte", "Embedded Systems", "machine learning, embedded systems, tinyml"},
	{"Accessible Interfaces for Screen Reader Users in Online Learning", 2022, domain.TypeDissertation, "Renata Batista", "Beatriz Ferraz", "Human-Computer Interaction", "accessibility, usability, education"},
	{"Gesture-Based Interaction in Virtual Reality Classrooms", 2023, domain.TypeTCC, "Felipe Rezende", "Jorge Albuquerque", "Human-Computer Interaction", "virtual reality, gesture interaction, education"},
	{"Federated Learning under Non-IID Data Distributions", 2024, domain.TypeThesis, "Aline Cavalcanti", "Helena Duarte", "Artificial Intelligence", "machine learning, federated learning, distributed systems"},
	{"Fault Injection Testing for Distributed Databases", 2020, domain.TypeTCC, "Rodrigo Pinto", "Sofia Menezes", "Databases", "fault injection, distributed systems, software testing"},
}

func (sw seedWork) work() domain.Work {
	return domain.Work{
		Title: sw.title, Year: sw.year, Type: sw.typ, Authors: []string{sw.author}, Advisor: sw.advisor,
		Areas: []string{sw.area}, Keywords: strings.Split(sw.keywords, ", "),
		Abstract: fmt.Sprintf("This %s, “%s”, reviews related work in %s, proposes an approach "+
			"and evaluates it experimentally. (Fictional demo record.)", strings.ToLower(sw.typ.Label()),
			sw.title, strings.ToLower(sw.area)),
		CreatedBy: "seed",
	}
}

// SeedIfEmpty creates demo users and works on a fresh database.
func (s *Store) SeedIfEmpty(ctx context.Context, docs *FileStore, now time.Time) error {
	for _, d := range DemoUsers {
		if err := s.AddUser(ctx, d.User, d.Password); err != nil {
			return err
		}
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM works`).Scan(&n); err != nil || n > 0 {
		return err
	}
	audit := func(action string) domain.AuditEntry {
		return domain.AuditEntry{Actor: "seed", Action: action, Detail: "demo data", At: now}
	}
	for i, sw := range seedPublished {
		w := sw.work()
		w.Status = domain.StatusPublished
		switch i % 3 {
		case 0:
			w.ExternalURL = fmt.Sprintf("https://repository.example.org/inf/handle/%d", 1000+i)
		case 1:
			name, err := docs.Save(samplePDF(w.Title))
			if err != nil {
				return err
			}
			w.PDFFile, w.PDFName = name, "full-text.pdf"
		} // case 2: no document source, to show the "unavailable" state
		if i%4 == 0 {
			w.ShelfLocation = fmt.Sprintf("INF Library, shelf %c-%02d", 'A'+i%5, i+1)
		}
		if i%5 == 0 {
			w.ContactInfo, w.ContactPublic = "Advisor office: INF building, room 2"+fmt.Sprint(10+i), true
		} else if i%5 == 1 {
			w.ContactInfo = "private: author's personal phone (must not be shown)"
		}
		if _, err := s.CreateWork(ctx, w, audit("SEED_PUBLISHED")); err != nil {
			return err
		}
	}

	draft := domain.Work{Title: "Quantum Circuit Simulation on GPUs", Status: domain.StatusDraft,
		Keywords: []string{"machine learning"}, CreatedBy: "library"}
	if _, err := s.CreateWork(ctx, draft, audit("CREATE_DRAFT")); err != nil {
		return err
	}

	pending := seedWork{"Explainable Machine Learning for Credit Scoring", 2025, domain.TypeDissertation,
		"Tiago Ramos", "Helena Duarte", "Artificial Intelligence", "machine learning, explainability, finance"}.work()
	pending.Status, pending.ExternalURL = domain.StatusUnderReview, "https://repository.example.org/inf/handle/2001"
	if _, err := s.CreateWork(ctx, pending, audit("SUBMIT")); err != nil {
		return err
	}

	rejected := seedWork{"Blockchain-Based Voting for Student Elections", 2024, domain.TypeTCC,
		"Clara Moreira", "Paulo Vasconcelos", "Theory of Computation", "blockchain, e-voting, security"}.work()
	rejected.Status, rejected.ExternalURL = domain.StatusUnderReview, "https://repository.example.org/inf/handle/2002"
	id, err := s.CreateWork(ctx, rejected, audit("SUBMIT"))
	if err != nil {
		return err
	}
	return s.RecordDecision(ctx, 1, domain.ReviewDecision{WorkID: id, Decision: domain.DecisionRejected,
		Reason:    "Advisor name does not match the defense record; please verify and resubmit.",
		Moderator: "coordinator", DecidedAt: now}, domain.StatusRejected)
}

// samplePDF builds a tiny valid one-page PDF so seeded downloads work.
func samplePDF(title string) []byte {
	esc := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`).Replace(title)
	content := fmt.Sprintf("BT /F1 14 Tf 50 780 Td (%s) Tj 0 -24 Td /F1 10 Tf (Fictional demo document - INF knowledge base prototype) Tj ET", esc)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// SamplePDF is exported for tests.
func SamplePDF(title string) []byte { return samplePDF(title) }
