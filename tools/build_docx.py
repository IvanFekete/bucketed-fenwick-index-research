"""Build the EEJET submission .docx from paper/manuscript.md.

Usage: python3 tools/build_docx.py [out.docx]

Layout follows the EEJET requirements (jet.com.ua, "Вимоги до оформлення"):
A4 portrait, 20 mm margins, Times New Roman 14 pt everywhere, single line
spacing, 0 pt before/after, no left/right indents, automatic hyphenation,
manual (non-automatic) lists, front matter in the order UDC - UA title -
UA authors - EN title - EN authors - UA abstract - UA keywords - EN abstract -
EN keywords, abstracts in italics, numbered equations with the number
right-aligned, references in the journal style (https://doi.org/...), both
a single "References" list (all sources are in English), author information as an English/Ukrainian
two-column table, and a block for potential reviewers.

The text itself is taken verbatim from the manuscript; only typesetting is
done here (equations, dashes between numbers, superscripts, list markers).
"""
import os
import re
import subprocess
import sys
import tempfile

from docx import Document
from docx.enum.table import WD_TABLE_ALIGNMENT
from docx.enum.text import WD_ALIGN_PARAGRAPH, WD_TAB_ALIGNMENT
from docx.oxml import OxmlElement
from docx.oxml.ns import qn
from docx.shared import Cm, Pt, RGBColor
from docx.text.run import Run

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SRC = os.path.join(ROOT, "paper", "manuscript.md")
FIG = os.path.join(ROOT, "figures")
OUT = sys.argv[1] if len(sys.argv) > 1 else os.path.join(ROOT, "paper", "manuscript_eejet.docx")

FONT = "Times New Roman"
SIZE = Pt(14)
UDC = "УДК 004.422.63"  # data structures; confirm with the editorial office

RESULTS_TITLE = "5. Results of evaluating the bucketed Fenwick index for matchmaking queues"
DISCUSSION_TITLE = "6. Discussion of results of evaluating the bucketed Fenwick index"

# Figures are drawn at their printed size (analysis/figures.py, analysis/fig_layout.py) so that
# their 14 pt lettering stays 14 pt in the document.
FIG_WIDTH = {"fig_model_vs_sim.png": "11cm"}

# Sources in Scopus Q1/Q2 journals or series (SCImago, SJR 2025, checked 01.11.2026), highlighted
# in purple as EEJET requires: Software: Practice and Experience Q2 [1, 2], Communications of the
# ACM Q1 [3], ACM Journal of Experimental Algorithmics Q2 [5], Lecture Notes in Computer Science
# Q2 [7], Journal of the Royal Statistical Society Series C Q2 [9], IEEE Transactions on Games Q2
# [15], Social Psychological and Personality Science Q1 [26], Proceedings of the ACM on Computer
# Graphics and Interactive Techniques Q1 [32]. Conference proceedings have no quartile; Acta
# Informatica is Q3; Journal of Pharmacokinetics and Biopharmaceutics is not indexed under that title.
Q12 = {1, 2, 3, 5, 7, 9, 15, 26, 32}
PURPLE = RGBColor(0x70, 0x30, 0xA0)

EQUATIONS = {
    "(1)": r"G_{\max}(\delta, b) = (\lfloor \delta / b \rfloor + 1) \cdot b - 1,",
    "(2)": r"P_0(b) = \begin{cases} (1 - \tau / b)^2, & b > \tau, \\ 0, & b \le \tau, \end{cases}",
    "(3)": (r"P_j(b) = \begin{cases} 0, & c \ge b, \\ \dfrac{(b - c)^2}{2b^2}, & 0 \le c < b, \\ "
            r"1 - \dfrac{(b + c)^2}{2b^2}, & -b < c < 0, \\ 1, & c \le -b. \end{cases}"),
}

# Inline formulas written in plain text in the manuscript -> TeX.
INLINE = [
    ("p= 1 / (1+10^(−Δ/400))", r"$p = 1/(1 + 10^{-\Delta/400})$"),
    ("|Δ|>τ≈400·log10(0.55/0.45)≈34.9", r"$|\Delta| > \tau \approx 400 \log_{10}(0.55/0.45) \approx 34.9$"),
    ("J=floor(δ/b)", r"$J = \lfloor \delta / b \rfloor$"),
    ("|j*b + U|", r"$|jb + U|$"),
    ("c = τ – j*b", r"$c = \tau - jb$"),
    ("Σ_j f_j * P_j(b)", r"$\sum_j f_j P_j(b)$"),
    ("(1 – τ/b)^2", r"$(1 - \tau/b)^2$"),
    ("(f_0 → 1)", r"($f_0 \to 1$)"),
    ("f_j is", r"$f_j$ is"),
    ("values of f_j", r"values of $f_j$"),
    ("G_max(δ, b)", r"$G_{\max}(\delta, b)$"),
]


def read_sections(md):
    """Split the manuscript into front matter pieces and the body."""
    lines = md.split("\n")
    get = lambda i: lines[i].strip()
    ua_title = get(0).lstrip("# ").strip()
    i_ua_abs = lines.index("## Анотація")
    ua_abs = [l for l in lines[i_ua_abs + 1:] if l.strip()][0]
    ua_kw = next(l for l in lines if l.startswith("Ключові слова:"))
    i_en = next(i for i, l in enumerate(lines) if l.startswith("# A bucketed"))
    en_title = lines[i_en].lstrip("# ").strip()
    en_author = [l for l in lines[i_en + 1:] if l.strip()][0].strip()
    i_en_abs = lines.index("## Abstract")
    en_abs = [l for l in lines[i_en_abs + 1:] if l.strip()][0]
    en_kw = next(l for l in lines if l.startswith("Keywords:"))
    i_body = lines.index("## 1. Introduction")
    i_refs = lines.index("## References")
    i_info = lines.index("## Information about the author")
    body = "\n".join(lines[i_body:i_refs])
    refs = [l for l in lines[i_refs + 1:i_info] if re.match(r"^\d+\. ", l)]
    return dict(ua_title=ua_title, ua_abs=ua_abs, ua_kw=ua_kw, en_title=en_title,
                en_author=en_author, en_abs=en_abs, en_kw=en_kw, body=body, refs=refs)


def typeset_text(s):
    """Typographic fixes that do not change wording."""
    s = s.replace(" ", " ")
    s = re.sub(r"(?<=\d)-(?=\d)", "–", s)          # 2-5 -> 2–5
    s = s.replace(" - ", " – ")
    s = re.sub(r"(\d)\^(\d+)", r"\1^\2^", s)         # 10^6 -> superscript
    return s


def protect_urls(s, fn):
    parts = re.split(r"(https?://\S+|\$[^$]+\$)", s)
    return "".join(p if (p.startswith("http") or p.startswith("$")) else fn(p) for p in parts)


def body_markdown(body):
    out = []
    for line in body.split("\n"):
        st = line.strip()
        if st == "## 5. Results":
            line = "## " + RESULTS_TITLE
        elif st == "## 6. Discussion of results":
            line = "## " + DISCUSSION_TITLE
        m = re.match(r"^(.*?),? \((\d)\)$", st)
        if m and m.group(2) and f"({m.group(2)})" in EQUATIONS and st.split("(")[0][:2] in ("G_", "P_"):
            out.append(f"EQN{m.group(2)}")
            continue
        for a, b in INLINE:
            line = line.replace(a, b)
        if re.match(r"^\s*- ", line):
            line = re.sub(r"^\s*- ", "– ", line)
        line = re.sub(r"^(\d+)\. ", r"\1\\. ", line)  # manual numbering, not Word lists
        m = re.match(r"^!\[[^\]]*\]\(\.\./figures/([^)]+)\)$", st)
        if m:
            name = m.group(1)
            width = FIG_WIDTH.get(name, "17cm")
            line = f"![]({os.path.join(FIG, name)}){{width={width}}}"
        elif not line.startswith("|"):
            line = protect_urls(line, typeset_text)
        else:
            line = protect_urls(line, typeset_text)
        out.append(line)
    return "\n".join(out)


def ref_markdown(refs):
    res = []
    for r in refs:
        r = re.sub(r"doi: (10\.\S+)", r"https://doi.org/\1", r)
        r = re.sub(r"^(\d+)\. ", r"\1\\. ", r)
        res.append(protect_urls(r, typeset_text))
    return "\n\n".join(res)


def build_markdown(d):
    md = []
    md.append(f"FRONT_UDC {UDC}")
    md.append(f"FRONT_TITLE {d['ua_title']}")
    md.append("FRONT_AUTHOR І. Фекете")
    md.append(f"FRONT_TITLE {d['en_title']}")
    md.append(f"FRONT_AUTHOR {d['en_author']}")
    md.append("*" + typeset_text(d["ua_abs"]).strip() + "*")
    kw_ua = d["ua_kw"].split(":", 1)
    md.append(f"**{kw_ua[0]}:**{kw_ua[1]}")
    md.append("*" + typeset_text(d["en_abs"]).strip() + "*")
    kw_en = d["en_kw"].split(":", 1)
    md.append(f"**{kw_en[0]}:**{kw_en[1]}")
    md.append(body_markdown(d["body"]))
    refs = ref_markdown(d["refs"])
    md.append("## References\n\n" + refs)
    md.append("## Information about the author\n\nAUTHOR_TABLE")
    md.append("## Potential reviewers\n\nREVIEWERS")
    return "\n\n".join(md)


# ----------------------------------------------------------------- post-processing

def set_run_font(run, bold=None, italic=None):
    run.font.name = FONT
    run.font.size = SIZE
    run.font.color.rgb = RGBColor(0, 0, 0)
    rpr = run._element.get_or_add_rPr()
    rfonts = rpr.find(qn("w:rFonts"))
    if rfonts is None:
        rfonts = OxmlElement("w:rFonts")
        rpr.insert(0, rfonts)
    for k in ("w:ascii", "w:hAnsi", "w:cs", "w:eastAsia"):
        rfonts.set(qn(k), FONT)
    if bold is not None:
        run.bold = bold
    if italic is not None:
        run.italic = italic


def fmt_par(p, align=WD_ALIGN_PARAGRAPH.JUSTIFY, first=Cm(1.25)):
    pf = p.paragraph_format
    pf.space_before = Pt(0)
    pf.space_after = Pt(0)
    pf.line_spacing = 1.0
    pf.left_indent = Cm(0)
    pf.right_indent = Cm(0)
    pf.first_line_indent = first
    p.alignment = align


def replace_paragraph_text(p, text, bold=False):
    for r in list(p.runs):
        r._element.getparent().remove(r._element)
    run = p.add_run(text)
    set_run_font(run, bold=bold)
    return run


def math_size(doc):
    """OMML runs inherit nothing from the paragraph; give them 14 pt explicitly."""
    for mr in doc.element.body.iter(qn("m:r")):
        rpr = mr.find(qn("w:rPr"))
        if rpr is None:
            rpr = OxmlElement("w:rPr")
            mrpr = mr.find(qn("m:rPr"))
            if mrpr is not None:
                mrpr.addnext(rpr)
            else:
                mr.insert(0, rpr)
        for tag in ("w:sz", "w:szCs"):
            el = rpr.find(qn(tag))
            if el is None:
                el = OxmlElement(tag)
                rpr.append(el)
            el.set(qn("w:val"), "28")


def column_widths(tbl, total_cm=17.0):
    """Width per column from its content: long words must fit, long text gets more room."""
    ncol = len(tbl.columns)
    longest = [0] * ncol
    total = [0] * ncol
    for row in tbl.rows:
        for j, cell in enumerate(row.cells[:ncol]):
            t = cell.text
            words = t.split() or [""]
            longest[j] = max(longest[j], max(len(w) for w in words))
            total[j] = max(total[j], len(t))
    char = 0.2  # cm per character, Times New Roman 14 pt (approx.)
    need = [max(1.0, l * char + 0.3) for l in longest]
    want = [max(n, (t ** 0.5) * char * 2.2) for n, t in zip(need, total)]
    if sum(need) >= total_cm:
        widths = [n * total_cm / sum(need) for n in need]
    else:
        room = total_cm - sum(need)
        widths = [n + room * w / sum(want) for n, w in zip(need, want)]
    tblPr = tbl._element.tblPr
    layout = tblPr.find(qn("w:tblLayout"))
    if layout is None:
        layout = OxmlElement("w:tblLayout")
        tblPr.append(layout)
    layout.set(qn("w:type"), "fixed")
    grid = tbl._element.find(qn("w:tblGrid"))
    for gc, w in zip(grid.findall(qn("w:gridCol")), widths):
        gc.set(qn("w:w"), str(int(w * 567)))
    for row in tbl.rows:
        for cell, w in zip(row.cells[:ncol], widths):
            cell.width = Cm(w)


def table_borders(tbl):
    tblPr = tbl._element.tblPr
    borders = OxmlElement("w:tblBorders")
    for edge in ("top", "left", "bottom", "right", "insideH", "insideV"):
        el = OxmlElement(f"w:{edge}")
        el.set(qn("w:val"), "single")
        el.set(qn("w:sz"), "4")
        el.set(qn("w:space"), "0")
        el.set(qn("w:color"), "000000")
        borders.append(el)
    old = tblPr.find(qn("w:tblBorders"))
    if old is not None:
        tblPr.remove(old)
    tblPr.append(borders)
    w = tblPr.find(qn("w:tblW"))
    if w is None:
        w = OxmlElement("w:tblW")
        tblPr.append(w)
    w.set(qn("w:type"), "dxa")
    w.set(qn("w:w"), str(int(17 * 567)))
    tbl.alignment = WD_TABLE_ALIGNMENT.CENTER
    mar = tblPr.find(qn("w:tblCellMar"))
    if mar is not None:
        tblPr.remove(mar)
    mar = OxmlElement("w:tblCellMar")
    for side in ("left", "right"):
        el = OxmlElement(f"w:{side}")
        el.set(qn("w:w"), "57")
        el.set(qn("w:type"), "dxa")
        mar.append(el)
    tblPr.append(mar)


def insert_table_after(doc, par, rows, header=None):
    doc_tbl = doc.add_table(rows=0, cols=len(rows[0]))
    if header:
        cells = doc_tbl.add_row().cells
        for c, h in zip(cells, header):
            c.paragraphs[0].add_run(h).bold = True
    for row in rows:
        cells = doc_tbl.add_row().cells
        for c, val in zip(cells, row):
            lines = val.split("\n")
            c.paragraphs[0].add_run(lines[0])
            for l in lines[1:]:
                c.add_paragraph().add_run(l)
    par._element.addnext(doc_tbl._element)
    par._element.getparent().remove(par._element)
    return doc_tbl


AUTHOR_EN = [
    "Ivan Fekete", "Master", "Department: –", "Google LLC",
    "1600 Amphitheatre Parkway, Mountain View, CA, USA, 94043", "E-mail: ivanfekete2607@gmail.com",
    "ORCID ID: https://orcid.org/0009-0007-4242-1398", "Contact phone: +1 (424) 336 6974",
    "Number of publications in national journals indexed: 1",
    "Number of publications in international journals indexed: 0",
    "H-index by Scopus: –", "ResearchGate: –", "Scopus ID: –",
    "Researcher ID: –", "Corresponding author",
]
AUTHOR_UA = [
    "Фекете Іван Іванович", "Магістр", "Кафедра: –", "Google LLC",
    "1600 Amphitheatre Parkway, Mountain View, CA, США, 94043", "E-mail: ivanfekete2607@gmail.com",
    "ORCID ID: https://orcid.org/0009-0007-4242-1398", "Контактний тел.: +1 (424) 336 6974",
    "Кількість статей в загальнодержавних базах даних: 1",
    "Кількість статей в міжнародних базах даних: 0",
    "H-index за Scopus: –", "ResearchGate: –", "Scopus ID: –",
    "Researcher ID: –", "Автор-кореспондент",
]
REVIEWERS = [
    ["[Full name]*", "[Degree, title]*", "[Phone]", "[E-mail]*", "[Work address]"],
    ["[Full name]*", "[Degree, title]*", "[Phone]", "[E-mail]*", "[Work address]"],
]


def _blank(p):
    from docx.text.paragraph import Paragraph
    e = OxmlElement("w:p")
    para = Paragraph(e, p._parent)
    fmt_par(para, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
    return e


def blank_before(p):
    """Empty line before a figure, so the figure block stands apart from the running text."""
    p._element.addprevious(_blank(p))


def blank_after(p):
    p._element.addnext(_blank(p))


def bold_label(p, label):
    """Make the caption label ("Fig. 3.", "Table 2.") bold; the caption text stays regular."""
    import copy
    first = p.runs[0]
    t = first.text
    if not t.startswith(label):
        return
    rest = t[len(label):]
    first.text = label
    first.bold = True
    if rest:
        new = copy.deepcopy(first._element)
        first._element.addnext(new)
        r2 = Run(new, p)
        r2.text = rest
        r2.bold = False


def tabbed_equation(p):
    pf = p.paragraph_format
    pf.tab_stops.add_tab_stop(Cm(8.5), WD_TAB_ALIGNMENT.CENTER)
    pf.tab_stops.add_tab_stop(Cm(17), WD_TAB_ALIGNMENT.RIGHT)


def postprocess(path):
    doc = Document(path)
    sec = doc.sections[0]
    sec.page_width, sec.page_height = Cm(21.0), Cm(29.7)
    for side in ("top_margin", "bottom_margin", "left_margin", "right_margin"):
        setattr(sec, side, Cm(2))
    sec.gutter = Cm(0)
    sec.header_distance = Cm(1.25)
    sec.footer_distance = Cm(1.25)

    for name in ("Normal", "Body Text", "First Paragraph", "Compact", "Heading 1", "Heading 2",
                 "Heading 3", "Title", "Author", "Abstract", "Table", "Image Caption", "Captioned Figure"):
        try:
            st = doc.styles[name]
        except KeyError:
            continue
        st.font.name, st.font.size = FONT, SIZE
        st.font.color.rgb = RGBColor(0, 0, 0)
        if st.paragraph_format is not None:
            st.paragraph_format.space_before = Pt(0)
            st.paragraph_format.space_after = Pt(0)
            st.paragraph_format.line_spacing = 1.0

    pars = list(doc.paragraphs)
    in_refs = False
    for p in pars:
        text = p.text
        style = p.style.name if p.style is not None else ""
        has_img = bool(p._element.findall(".//" + qn("w:drawing")))
        has_math = bool(p._element.findall(".//" + qn("m:oMath")))
        if text.startswith("FRONT_UDC "):
            replace_paragraph_text(p, text[len("FRONT_UDC "):], bold=True)
            fmt_par(p, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
            continue
        if text.startswith("FRONT_TITLE "):
            replace_paragraph_text(p, text[len("FRONT_TITLE "):], bold=True)
            fmt_par(p, WD_ALIGN_PARAGRAPH.CENTER, Cm(0))
            continue
        if text.startswith("FRONT_AUTHOR "):
            replace_paragraph_text(p, text[len("FRONT_AUTHOR "):], bold=True)
            fmt_par(p, WD_ALIGN_PARAGRAPH.CENTER, Cm(0))
            continue
        if text == "AUTHOR_TABLE":
            t = insert_table_after(doc, p, [["\n".join(AUTHOR_EN), "\n".join(AUTHOR_UA)]], header=["Англ.", "Укр."])
            table_borders(t)
            continue
        if text == "REVIEWERS":
            anchor = p
            lines = []
            for k in (1, 2):
                lines += [f"Reviewer {k}", "Full name*:", "Degree, title*:", "Phone:",
                          "E-mail*:", "Work address:"]
            for i, l in enumerate(lines):
                np_ = anchor.insert_paragraph_before(l) if i == 0 else None
                if np_ is None:
                    np_ = doc.add_paragraph(l)
                    prev._element.addnext(np_._element)
                fmt_par(np_, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
                for r in np_.runs:
                    set_run_font(r, bold=l.startswith("Reviewer"))
                prev = np_
            p._element.getparent().remove(p._element)
            continue
        if style.startswith("Heading"):
            in_refs = p.text.strip() in ("Література", "References")
            for r in p.runs:
                set_run_font(r, bold=True, italic=False)
            fmt_par(p, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
            continue
        if has_img:
            fmt_par(p, WD_ALIGN_PARAGRAPH.CENTER, Cm(0))
            p.paragraph_format.keep_with_next = True
            blank_before(p)
            continue
        if has_math and text.strip().startswith("EQN"):
            pass
        if re.match(r"^(Fig\.|Table) \d+\. ", text):
            fmt_par(p, WD_ALIGN_PARAGRAPH.CENTER, Cm(0))
            if text.startswith("Table"):
                p.paragraph_format.keep_with_next = True
        elif re.match(r"^\d+\. ", text) and p._element.getprevious() is not None and False:
            fmt_par(p)
        elif in_refs:
            fmt_par(p, WD_ALIGN_PARAGRAPH.LEFT)
        else:
            fmt_par(p)
        for r in p.runs:
            set_run_font(r)
        cap = re.match(r"^((?:Fig\.|Table) \d+\.)", text)
        if cap:
            bold_label(p, cap.group(1))
            if text.startswith("Fig."):
                blank_after(p)
        m = re.match(r"^(\d+)\. ", text)
        if in_refs and m and int(m.group(1)) in Q12:
            for rel in p._element.iter(qn("w:r")):
                Run(rel, p).font.color.rgb = PURPLE

    # Equations: paragraphs whose only text is the marker.
    for p in doc.paragraphs:
        if re.fullmatch(r"EQN\d", p.text.strip()):
            n = p.text.strip()[3:]
            p._element.getparent().replace(p._element, EQ_PARAS[n])

    math_size(doc)
    for t in doc.tables:
        table_borders(t)
        column_widths(t)
        tseq = ["tblStyle", "tblpPr", "tblOverlap", "bidiVisual", "tblStyleRowBandSize", "tblStyleColBandSize",
                "tblW", "jc", "tblCellSpacing", "tblInd", "tblBorders", "shd", "tblLayout", "tblCellMar",
                "tblLook", "tblCaption", "tblDescription"]
        tpr = t._element.tblPr
        kids = sorted(list(tpr), key=lambda c: tseq.index(c.tag.split("}")[1]) if c.tag.split("}")[1] in tseq else 99)
        for c in kids:
            tpr.remove(c)
            tpr.append(c)
        for row in t.rows:
            for cell in row.cells:
                for p in cell.paragraphs:
                    fmt_par(p, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
                    for r in p.runs:
                        set_run_font(r)

    settings = doc.settings.element
    if settings.find(qn("w:autoHyphenation")) is None:
        hy = OxmlElement("w:autoHyphenation")
        anchor = settings.find(qn("w:defaultTabStop"))
        if anchor is not None:
            anchor.addnext(hy)
        else:
            later = [qn("w:consecutiveHyphenLimit"), qn("w:hyphenationZone"), qn("w:doNotHyphenateCaps"),
                     qn("w:characterSpacingControl"), qn("w:compat"), qn("w:rsids"), qn("m:mathPr"),
                     qn("w:themeFontLang"), qn("w:clrSchemeMapping"), qn("w:shapeDefaults"),
                     qn("w:decimalSymbol"), qn("w:listSeparator")]
            nxt = next((c for c in settings if c.tag in later), None)
            if nxt is not None:
                nxt.addprevious(hy)
            else:
                settings.append(hy)
    seq = ("writeProtection view zoom removePersonalInformation removeDateAndTime doNotDisplayPageBoundaries "
           "displayBackgroundShape printPostScriptOverText printFractionalCharacterWidth printFormsData "
           "embedTrueTypeFonts embedSystemFonts saveSubsetFonts saveFormsData mirrorMargins alignBordersAndEdges "
           "bordersDoNotSurroundHeader bordersDoNotSurroundFooter gutterAtTop hideSpellingErrors "
           "hideGrammaticalErrors activeWritingStyle proofState formsDesign attachedTemplate linkStyles "
           "stylePaneFormatFilter stylePaneSortMethod documentType mailMerge revisionView trackRevisions "
           "doNotTrackMoves doNotTrackFormatting documentProtection autoFormatOverride styleLockTheme "
           "styleLockQFSet defaultTabStop autoHyphenation consecutiveHyphenLimit hyphenationZone "
           "doNotHyphenateCaps showEnvelope summaryLength clickAndTypeStyle defaultTableStyle evenAndOddHeaders "
           "bookFoldRevPrinting bookFoldPrinting bookFoldPrintingSheets drawingGridHorizontalSpacing "
           "drawingGridVerticalSpacing displayHorizontalDrawingGridEvery displayVerticalDrawingGridEvery "
           "doNotUseMarginsForDrawingGridOrigin drawingGridHorizontalOrigin drawingGridVerticalOrigin "
           "doNotShadeFormData noPunctuationKerning characterSpacingControl printTwoOnOne strictFirstAndLastChars "
           "noLineBreaksAfter noLineBreaksBefore savePreviewPicture doNotValidateAgainstSchema saveInvalidXml "
           "ignoreMixedContent alwaysShowPlaceholderText doNotDemarcateInvalidXml saveXmlDataOnly "
           "useXSLTWhenSaving saveThroughXslt showXMLTags alwaysMergeEmptyNamespace updateFields hdrShapeDefaults "
           "footnotePr endnotePr compat docVars rsids mathPr attachedSchema themeFontLang clrSchemeMapping "
           "doNotIncludeSubdocsInStats doNotAutoCompressPictures forceUpgrade captions readModeInkLockDown "
           "smartTagType schemaLibrary shapeDefaults doNotEmbedSmartTags decimalSymbol listSeparator").split()
    def rank(c):
        local = c.tag.split("}")[1]
        return seq.index(local) if local in seq else len(seq)
    kids = sorted(list(settings), key=rank)
    for c in kids:
        settings.remove(c)
        settings.append(c)
    for mcpr in doc.element.body.iter(qn("m:mcPr")):
        kids = sorted(list(mcpr), key=lambda c: 0 if c.tag == qn("m:count") else 1)
        for c in kids:
            mcpr.remove(c)
            mcpr.append(c)
    order = [qn("m:begChr"), qn("m:sepChr"), qn("m:endChr"), qn("m:grow"), qn("m:shp"), qn("m:ctrlPr")]
    for dpr in doc.element.body.iter(qn("m:dPr")):
        kids = sorted(list(dpr), key=lambda c: order.index(c.tag) if c.tag in order else len(order))
        for c in kids:
            dpr.remove(c)
            dpr.append(c)
    doc.core_properties.title = "A bucketed Fenwick index for skill-based matchmaking queues"
    doc.core_properties.author = "Ivan Fekete"
    doc.save(path)


EQ_PARAS = {}


def equation_paragraphs(tmp):
    """Render each numbered equation as: <tab> OMML <tab> (n), right-aligned number."""
    for n, tex in EQUATIONS.items():
        k = n.strip("()")
        md = os.path.join(tmp, f"eq{k}.md")
        out = os.path.join(tmp, f"eq{k}.docx")
        open(md, "w").write(f"EQTAB${tex}$EQTAB({k})\n")
        subprocess.run(["pandoc", md, "-o", out], check=True)
        d = Document(out)
        p = d.paragraphs[0]
        for r in p.runs:
            if "EQTAB" in r.text:
                r.text = r.text.replace("EQTAB", "\t")
            set_run_font(r)
        fmt_par(p, WD_ALIGN_PARAGRAPH.LEFT, Cm(0))
        tabbed_equation(p)
        EQ_PARAS[k] = p._element


def main():
    md = open(SRC, encoding="utf-8").read()
    d = read_sections(md)
    full = build_markdown(d)
    with tempfile.TemporaryDirectory() as tmp:
        src = os.path.join(tmp, "in.md")
        open(src, "w", encoding="utf-8").write(full)
        subprocess.run(["pandoc", src, "-f", "markdown+superscript-implicit_figures-auto_identifiers",
                        "-o", OUT], check=True)
        equation_paragraphs(tmp)
        postprocess(OUT)
    print("wrote", OUT)


if __name__ == "__main__":
    main()
